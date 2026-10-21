package fetcher

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/floatpane/matcha/config"
)

func TestMailRuleConditionSemantics(t *testing.T) {
	envelope := &imap.Envelope{
		From:    []imap.Address{{Name: "News Desk", Mailbox: "sender", Host: "example.com"}},
		To:      []imap.Address{{Name: "Primary User", Mailbox: "me", Host: "example.com"}},
		Cc:      []imap.Address{{Name: "Copy User", Mailbox: "copy", Host: "example.com"}},
		Bcc:     []imap.Address{{Mailbox: "hidden", Host: "example.com"}},
		Subject: "Weekly Café REPORT",
	}
	cases := []struct {
		name string
		rule config.MailRule
		want bool
	}{
		{"case insensitive sender substring", config.MailRule{Enabled: true, From: "SENDER@EXAMPLE"}, true},
		{"sender display name", config.MailRule{Enabled: true, From: "news desk"}, true},
		{"recipient display name", config.MailRule{Enabled: true, To: "PRIMARY USER"}, true},
		{"copy recipient convention", config.MailRule{Enabled: true, To: "copy@example"}, true},
		{"bcc is not To", config.MailRule{Enabled: true, To: "hidden@example"}, false},
		{"unicode subject", config.MailRule{Enabled: true, Subject: "CAFÉ report"}, true},
		{"all conditions", config.MailRule{Enabled: true, From: "sender", To: "me@example", Subject: "weekly"}, true},
		{"AND not OR", config.MailRule{Enabled: true, From: "sender", To: "someone-else", Subject: "weekly"}, false},
		{"literal substring not regex", config.MailRule{Enabled: true, Subject: "Weekly.*REPORT"}, false},
		{"disabled", config.MailRule{Enabled: false, From: "sender"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ruleMatchesEnvelope(tc.rule, envelope); got != tc.want {
				t.Fatalf("matches=%t want=%t", got, tc.want)
			}
		})
	}
	if ruleMatchesEnvelope(config.MailRule{Enabled: true, From: "sender"}, nil) {
		t.Fatal("missing envelope matched")
	}
}

type mailRuleServerState struct {
	mu            sync.Mutex
	actions       []string
	failMoveUID   imap.UID
	failStoreUID  imap.UID
	validity      uint32
	badFetch      bool
	globalExpunge bool
}

type mailRuleSession struct {
	*imapmemserver.UserSession
	state *mailRuleServerState
}

func (session *mailRuleSession) Select(mailbox string, options *imap.SelectOptions) (*imap.SelectData, error) {
	selected, err := session.UserSession.Select(mailbox, options)
	if err == nil {
		session.state.mu.Lock()
		if session.state.validity != 0 {
			selected.UIDValidity = session.state.validity
		}
		session.state.mu.Unlock()
	}
	return selected, err
}

func (session *mailRuleSession) Fetch(w *imapserver.FetchWriter, set imap.NumSet, options *imap.FetchOptions) error {
	session.state.mu.Lock()
	if _, ok := set.(imap.UIDSet); !ok {
		session.state.badFetch = true
	}
	if options.BodyStructure != nil || len(options.BinarySection) != 0 {
		session.state.badFetch = true
	}
	for _, section := range options.BodySection {
		if section.Specifier != imap.PartSpecifierHeader || !section.Peek {
			session.state.badFetch = true
		}
		for _, header := range section.HeaderFields {
			if header != "Delivered-To" && header != "X-Forwarded-To" && header != "X-Original-To" {
				session.state.badFetch = true
			}
		}
	}
	session.state.mu.Unlock()
	return session.UserSession.Fetch(w, set, options)
}

func (session *mailRuleSession) Store(w *imapserver.FetchWriter, set imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	session.state.mu.Lock()
	uidSet, ok := set.(imap.UIDSet)
	if !ok {
		session.state.mu.Unlock()
		return errors.New("rules must use UID STORE")
	}
	fail := session.state.failStoreUID != 0 && uidSet.Contains(session.state.failStoreUID)
	session.state.actions = append(session.state.actions, "store "+set.String())
	session.state.mu.Unlock()
	if fail {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "isolated server rejected STORE"}
	}
	return session.UserSession.Store(w, set, flags, options)
}

func (session *mailRuleSession) Move(w *imapserver.MoveWriter, set imap.NumSet, destination string) error {
	session.state.mu.Lock()
	uidSet, ok := set.(imap.UIDSet)
	if !ok {
		session.state.mu.Unlock()
		return errors.New("rules must use UID MOVE")
	}
	fail := session.state.failMoveUID != 0 && uidSet.Contains(session.state.failMoveUID)
	session.state.actions = append(session.state.actions, "move "+set.String())
	session.state.mu.Unlock()
	if fail {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "private server response must not escape"}
	}
	return session.UserSession.Move(w, set, destination)
}

func (session *mailRuleSession) Expunge(w *imapserver.ExpungeWriter, set *imap.UIDSet) error {
	if set == nil {
		session.state.mu.Lock()
		session.state.globalExpunge = true
		session.state.mu.Unlock()
		return errors.New("global expunge forbidden")
	}
	return session.UserSession.Expunge(w, set)
}

func mailRuleIMAP(t *testing.T, move bool) (*imapclient.Client, *mailRuleServerState) {
	t.Helper()
	user := imapmemserver.NewUser("me@example.com", "isolated-password")
	for _, folder := range []string{"INBOX", "News"} {
		if err := user.Create(folder, nil); err != nil {
			t.Fatal(err)
		}
	}
	state := &mailRuleServerState{}
	caps := imap.CapSet{imap.CapIMAP4rev1: {}}
	if move {
		caps[imap.CapMove] = struct{}{}
	}
	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &mailRuleSession{UserSession: imapmemserver.NewUserSession(user), state: state}, nil, nil
		},
		Caps:         caps,
		InsecureAuth: true,
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)            //nolint:errcheck
	t.Cleanup(func() { server.Close() }) //nolint:errcheck
	client, err := imapclient.DialInsecure(listener.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() }) //nolint:errcheck
	if err := client.Login("me@example.com", "isolated-password").Wait(); err != nil {
		t.Fatal(err)
	}
	return client, state
}

func runRuleBatch(c *imapclient.Client, account *config.Account, rules []config.MailRule, cursor MailRuleProgress) (MailRuleBatch, error) {
	batch := MailRuleBatch{Progress: cursor}
	var operation, ruleID string
	var uid uint32
	err := applyMailRulesOnClient(c, account, rules, &batch, &operation, &ruleID, &uid)
	return batch, err
}

func testReadRule() config.MailRule {
	return config.MailRule{ID: "read-news", Name: "Read News", Enabled: true, Subject: "news", MarkRead: true}
}

func TestMailRulesBaselineBatchesAndUIDReset(t *testing.T) {
	client, state := mailRuleIMAP(t, true)
	account := &config.Account{ID: "account", Email: "me@example.com"}
	rules := []config.MailRule{testReadRule()}
	appendMessage(t, client, "INBOX", "me@example.com", "old news")
	baseline, err := runRuleBatch(client, account, rules, MailRuleProgress{})
	if err != nil || baseline.Changed || baseline.Processed != 0 || baseline.Progress.LastUID != 1 || baseline.More {
		t.Fatalf("historical mail was not baselined safely: %+v %v", baseline, err)
	}
	for i := range 101 {
		appendMessage(t, client, "INBOX", "me@example.com", fmt.Sprintf("news %d", i))
	}
	first, err := runRuleBatch(client, account, rules, baseline.Progress)
	if err != nil || !first.Changed || first.Processed != 100 || first.Progress.LastUID != 101 || !first.More {
		t.Fatalf("first batch: %+v %v", first, err)
	}
	second, err := runRuleBatch(client, account, rules, first.Progress)
	if err != nil || !second.Changed || second.Processed != 1 || second.Progress.LastUID != 102 || second.More {
		t.Fatalf("second batch: %+v %v", second, err)
	}
	again, err := runRuleBatch(client, account, rules, second.Progress)
	if err != nil || again.Changed || again.Processed != 0 || again.More {
		t.Fatalf("completed cursor replayed: %+v %v", again, err)
	}
	messages, err := client.Fetch(imap.UIDSetNum(1), &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil || len(messages) != 1 || hasSeenFlag(messages[0].Flags) {
		t.Fatalf("pre-activation message was changed: %+v %v", messages, err)
	}
	appendMessage(t, client, "INBOX", "me@example.com", "news before UID reset")
	state.mu.Lock()
	state.validity = second.Progress.UIDValidity + 1
	state.mu.Unlock()
	reset, err := runRuleBatch(client, account, rules, second.Progress)
	if err != nil || reset.Changed || reset.Processed != 0 || reset.Progress.LastUID != 103 || reset.Progress.UIDValidity == second.Progress.UIDValidity {
		t.Fatalf("UID reset must baseline without historical actions: %+v %v", reset, err)
	}
	appendMessage(t, client, "INBOX", "me@example.com", "news after UID reset")
	afterReset, err := runRuleBatch(client, account, rules, reset.Progress)
	if err != nil || !afterReset.Changed || afterReset.Processed != 1 || afterReset.Progress.LastUID != 104 {
		t.Fatalf("future arrivals after UID reset: %+v %v", afterReset, err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.badFetch || state.globalExpunge {
		t.Fatalf("unsafe protocol operations: body fetch=%t global expunge=%t", state.badFetch, state.globalExpunge)
	}
}

func TestMailRulesPartialFailureKeepsSafePrefixAndMarksBeforeMove(t *testing.T) {
	client, state := mailRuleIMAP(t, true)
	account := &config.Account{ID: "account", Email: "me@example.com"}
	rule := testReadRule()
	rule.MoveTo = "News"
	baseline, err := runRuleBatch(client, account, []config.MailRule{rule}, MailRuleProgress{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		appendMessage(t, client, "INBOX", "me@example.com", fmt.Sprintf("news %d", i))
	}
	state.mu.Lock()
	state.failMoveUID = 2
	state.mu.Unlock()
	partial, err := runRuleBatch(client, account, []config.MailRule{rule}, baseline.Progress)
	if err == nil || !partial.Changed || partial.Progress.LastUID != 1 || partial.Processed != 1 {
		t.Fatalf("failed UID or later mail was checkpointed: %+v %v", partial, err)
	}
	state.mu.Lock()
	if !slices.Equal(state.actions, []string{"store 1", "move 1", "store 2", "move 2"}) {
		t.Errorf("actions were not ordered mark-read before MOVE: %v", state.actions)
	}
	state.failMoveUID = 0
	state.mu.Unlock()
	resumed, err := runRuleBatch(client, account, []config.MailRule{rule}, partial.Progress)
	if err != nil || !resumed.Changed || resumed.Progress.LastUID != 3 || resumed.Processed != 2 {
		t.Fatalf("failed UID was not retried: %+v %v", resumed, err)
	}
	if _, err := client.Select("News", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	messages, err := client.Fetch(imap.UIDSetNum(1, 2, 3), &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil || len(messages) != 3 {
		t.Fatalf("move lost/duplicated mail: messages=%d err=%v", len(messages), err)
	}
	for _, message := range messages {
		if !hasSeenFlag(message.Flags) {
			t.Fatalf("moved UID %d was not marked read first", message.UID)
		}
	}
}

func TestMailRulesStoreFailureDoesNotAdvanceOrMove(t *testing.T) {
	client, state := mailRuleIMAP(t, true)
	account := &config.Account{ID: "account", Email: "me@example.com"}
	rule := testReadRule()
	rule.MoveTo = "News"
	baseline, err := runRuleBatch(client, account, []config.MailRule{rule}, MailRuleProgress{})
	if err != nil {
		t.Fatal(err)
	}
	appendMessage(t, client, "INBOX", "me@example.com", "news")
	state.mu.Lock()
	state.failStoreUID = 1
	state.mu.Unlock()
	failed, err := runRuleBatch(client, account, []config.MailRule{rule}, baseline.Progress)
	if err == nil || failed.Changed || failed.Progress.LastUID != 0 || failed.Processed != 0 {
		t.Fatalf("failed STORE advanced checkpoint: %+v %v", failed, err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !slices.Equal(state.actions, []string{"store 1"}) {
		t.Fatalf("MOVE followed failed STORE: %v", state.actions)
	}
}

func TestMailRulesRequireMoveWithoutUnsafeFallback(t *testing.T) {
	client, state := mailRuleIMAP(t, false)
	account := &config.Account{ID: "account", Email: "me@example.com"}
	rule := testReadRule()
	rule.MoveTo = "News"
	baseline, err := runRuleBatch(client, account, []config.MailRule{rule}, MailRuleProgress{})
	if err != nil {
		t.Fatal(err)
	}
	appendMessage(t, client, "INBOX", "me@example.com", "news", imap.FlagDeleted)
	failed, err := runRuleBatch(client, account, []config.MailRule{rule}, baseline.Progress)
	if !errors.Is(err, errMailRuleMoveUnsupported) || failed.Changed || failed.Progress.LastUID != 0 {
		t.Fatalf("missing MOVE was not rejected before mutation: %+v %v", failed, err)
	}
	state.mu.Lock()
	if len(state.actions) != 0 || state.globalExpunge {
		t.Errorf("unsafe fallback mutated mail: %v global=%t", state.actions, state.globalExpunge)
	}
	state.mu.Unlock()
	rule.MoveTo = ""
	read, err := runRuleBatch(client, account, []config.MailRule{rule}, baseline.Progress)
	if err != nil || !read.Changed || read.Progress.LastUID != 1 {
		t.Fatalf("mark-read-only needs no MOVE capability: %+v %v", read, err)
	}
}

func TestMailRulesAccountDeliveryFilteringAndFirstMatch(t *testing.T) {
	client, _ := mailRuleIMAP(t, true)
	account := &config.Account{ID: "account", Email: "login@example.com", FetchEmail: "alias@example.com"}
	first, second := testReadRule(), testReadRule()
	second.ID, second.MoveTo = "second", "News"
	rules := []config.MailRule{first, second}
	baseline, err := runRuleBatch(client, account, rules, MailRuleProgress{})
	if err != nil {
		t.Fatal(err)
	}
	appendMessage(t, client, "INBOX", "other@example.com", "news other alias")
	appendMessage(t, client, "INBOX", "alias@example.com", "news mine")
	raw := "From: sender@example.com\r\nTo: original@example.net\r\nDelivered-To: alias@example.com\r\nSubject: news forwarded\r\n\r\nbody must not be fetched\r\n"
	cmd := client.Append("INBOX", int64(len(raw)), nil)
	if _, err := cmd.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	batch, err := runRuleBatch(client, account, rules, baseline.Progress)
	if err != nil || batch.Processed != 3 || !batch.Changed {
		t.Fatalf("delivery filtering: %+v %v", batch, err)
	}
	messages, err := client.Fetch(imap.UIDSetNum(1, 2, 3), &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil || len(messages) != 3 {
		t.Fatalf("later matching MOVE must not run: messages=%d err=%v", len(messages), err)
	}
	for _, message := range messages {
		if hasSeenFlag(message.Flags) != (message.UID != 1) {
			t.Fatalf("delivery scope mishandled UID %d flags=%v", message.UID, message.Flags)
		}
	}
	account.CatchAll = true
	appendMessage(t, client, "INBOX", "other@example.com", "news catch-all")
	catchAll, err := runRuleBatch(client, account, rules, batch.Progress)
	if err != nil || !catchAll.Changed || catchAll.Processed != 1 {
		t.Fatalf("CatchAll did not include the other delivery address: %+v %v", catchAll, err)
	}
}

func TestApplyMailRulesScopesUnsupportedProtocolsAndSanitizesErrors(t *testing.T) {
	client, state := mailRuleIMAP(t, true)
	previousPool := imapPool
	pool := newClientPool(func(*config.Account) (*imapclient.Client, error) { return client, nil }, time.Minute)
	imapPool = pool
	t.Cleanup(func() { pool.closeIdle(); imapPool = previousPool })
	account := &config.Account{ID: "account", Protocol: "imap", Email: "me@example.com"}
	rule := testReadRule()
	rule.MoveTo = "News"
	baseline, err := ApplyMailRules(account, []config.MailRule{rule}, MailRuleProgress{})
	if err != nil {
		t.Fatal(err)
	}
	appendMessage(t, client, "INBOX", "me@example.com", "news")
	state.mu.Lock()
	state.failMoveUID = 1
	state.mu.Unlock()
	partial, err := ApplyMailRules(account, []config.MailRule{rule}, baseline.Progress)
	if err == nil || !partial.Changed || partial.Progress.LastUID != 0 || !strings.Contains(err.Error(), "rule \"read-news\"") || strings.Contains(err.Error(), "private server response") {
		t.Fatalf("partial result/error context was lost or leaked: %+v %v", partial, err)
	}
	for _, protocol := range []string{"pop3", "jmap", "maildir", "unknown"} {
		account.Protocol = protocol
		if _, err := ApplyMailRules(account, []config.MailRule{rule}, baseline.Progress); err == nil {
			t.Fatalf("unsupported protocol %q was dispatched as IMAP", protocol)
		}
	}
	account.Protocol = "unknown"
	rule.AccountID = "different-account"
	if batch, err := ApplyMailRules(account, []config.MailRule{rule}, baseline.Progress); err != nil || batch.Progress != baseline.Progress || batch.Changed {
		t.Fatalf("unrelated rules should do nothing even on unsupported accounts: %+v %v", batch, err)
	}
}

func TestMailRulesMissingDestinationPreservesFailedUID(t *testing.T) {
	client, state := mailRuleIMAP(t, true)
	account := &config.Account{ID: "account", Email: "me@example.com"}
	rule := testReadRule()
	rule.MoveTo = "MissingDestination"
	baseline, err := runRuleBatch(client, account, []config.MailRule{rule}, MailRuleProgress{})
	if err != nil {
		t.Fatal(err)
	}
	appendMessage(t, client, "INBOX", "me@example.com", "news")
	batch, err := runRuleBatch(client, account, []config.MailRule{rule}, baseline.Progress)
	if err == nil || !batch.Changed || batch.Progress.LastUID != 0 || batch.Processed != 0 {
		t.Fatalf("missing destination lost partial result or advanced the failed UID: %+v %v", batch, err)
	}
	if _, err := client.Select("MissingDestination", nil).Wait(); err == nil {
		t.Fatal("rule silently created the destination")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.globalExpunge {
		t.Fatal("missing destination triggered global expunge")
	}
}
