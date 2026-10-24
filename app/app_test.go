package app

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
	"github.com/floatpane/matcha/internal/emailactions"
	"github.com/floatpane/matcha/internal/emailstore"
	"github.com/floatpane/matcha/internal/palette"
	"github.com/floatpane/matcha/internal/pluginbridge"
	"github.com/floatpane/matcha/tui"
	"github.com/zalando/go-keyring"
)

func TestOpeningUnreadEmailUpdatesVisibleFilter(t *testing.T) {
	accounts := []config.Account{{ID: "a", FetchEmail: "a@example.com"}}
	email := fetcher.Email{UID: 7, AccountID: "a", Subject: "unread"}
	store := emailstore.NewStore()
	store.SetFolder("INBOX", []fetcher.Email{email})
	folders := tui.NewFolderInbox([]string{"INBOX"}, accounts)
	folders.SetEmails([]fetcher.Email{email}, accounts)
	folders.Update(tea.KeyPressMsg{Code: 'U', Text: "U"})
	m := &Model{config: &config.Config{Accounts: accounts}, store: store, folderInbox: folders}
	m.markEmailReadAndQueue("a", 7, false)

	if selected := folders.GetInbox().GetSelectedEmail(); selected != nil {
		t.Fatalf("opened message remains visible in unread view: %+v", selected)
	}
	if got := store.GetEmailByUIDAndAccount(7, "a"); got == nil || !got.IsRead {
		t.Fatalf("opened message was not marked read in the store: %+v", got)
	}
}

func TestUnreadPaginationContinuesThroughApp(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	accounts := []config.Account{{ID: "a", FetchEmail: "a@example.com"}}
	folders := tui.NewFolderInbox([]string{"INBOX"}, accounts)
	folders.Update(tea.KeyPressMsg{Code: 'U', Text: "U"})
	m := &Model{
		current: folders, config: &config.Config{Accounts: accounts},
		store: emailstore.NewStore(), folderInbox: folders, palette: palette.New(),
	}
	_, cmd := m.Update(tui.FolderEmailsAppendedMsg{
		FolderName: "INBOX", AccountID: "a",
		Emails: []fetcher.Email{{UID: 7, AccountID: "a", IsRead: true}},
	})
	if cmd == nil {
		t.Fatal("app dropped unread pagination request")
	}
	fetch, ok := cmd().(tui.FetchFolderMoreEmailsMsg)
	if !ok || fetch.FolderName != "INBOX" || fetch.AccountID != "a" || fetch.Offset != 1 {
		t.Fatalf("app follow-up fetch = %#v, want next INBOX page", fetch)
	}
}

func TestUnreadInitialFolderFetchRequestsOlderPage(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	accounts := []config.Account{{ID: "a", FetchEmail: "a@example.com"}}
	cfg := &config.Config{Accounts: accounts}
	store := emailstore.NewStore()
	folders := tui.NewFolderInbox([]string{"INBOX"}, accounts)
	folders.Update(tea.KeyPressMsg{Code: 'U', Text: "U"})
	m := &Model{
		current: folders, config: cfg, store: store, folderInbox: folders,
		palette: palette.New(), pluginBridge: pluginbridge.NewManager(nil, store, cfg, folders),
	}
	_, cmd := m.Update(tui.FolderEmailsFetchedMsg{
		FolderName: "INBOX",
		Emails:     []fetcher.Email{{UID: 7, AccountID: "a", IsRead: true}},
	})
	if cmd == nil {
		t.Fatal("initial read-only page should request older messages")
	}
	var fetch tui.FetchFolderMoreEmailsMsg
	var found bool
	for _, nested := range []tea.Cmd{cmd} {
		switch msg := nested().(type) {
		case tui.FetchFolderMoreEmailsMsg:
			fetch, found = msg, true
		case tea.BatchMsg:
			for _, inner := range msg {
				if request, ok := inner().(tui.FetchFolderMoreEmailsMsg); ok {
					fetch, found = request, true
				}
			}
		}
	}
	if !found || fetch.FolderName != "INBOX" || fetch.AccountID != "a" || fetch.Offset != 1 {
		t.Fatalf("initial folder fetch follow-up = %#v (found %t), want older INBOX page", fetch, found)
	}
}

func TestFolderFetchFailureKeepsAccountEmailsAndSaysSo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	accounts := []config.Account{
		{ID: "a", Email: "a@example.com", FetchEmail: "a@example.com"},
		{ID: "b", Email: "b@example.com", FetchEmail: "b@example.com"},
	}
	cfg := &config.Config{Accounts: accounts}
	store := emailstore.NewStore()
	store.FolderEmails["INBOX"] = []fetcher.Email{
		{UID: 1, AccountID: "a", Subject: "old a"},
		{UID: 2, AccountID: "b", Subject: "old b"},
	}
	folders := tui.NewFolderInbox([]string{"INBOX"}, accounts)
	folders.Update(tea.WindowSizeMsg{Width: 400, Height: 40})
	m := &Model{
		current: folders, config: cfg, store: store, folderInbox: folders,
		palette: palette.New(), pluginBridge: pluginbridge.NewManager(nil, store, cfg, folders),
	}
	title := func() string { return ansi.Strip(folders.GetInbox().View().Content) }

	m.Update(tui.FolderEmailsFetchedMsg{
		FolderName:     "INBOX",
		Emails:         []fetcher.Email{{UID: 3, AccountID: "a", Subject: "new a"}},
		FailedAccounts: map[string]string{"b": "connection reset"},
	})
	subjects := make([]string, 0, len(store.FolderEmails["INBOX"]))
	for _, email := range store.FolderEmails["INBOX"] {
		subjects = append(subjects, email.Subject)
	}
	if !slices.Contains(subjects, "old b") || !slices.Contains(subjects, "new a") || slices.Contains(subjects, "old a") {
		t.Fatalf("folder after failed fetch = %v, want new a plus the last known b", subjects)
	}
	if !strings.Contains(title(), "could not load b@example.com") {
		t.Fatalf("inbox does not say which account failed:\n%s", title())
	}

	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if !strings.Contains(title(), "refreshing") {
		t.Fatalf("pressing r did not start a refresh:\n%s", title())
	}
	m.Update(tui.FolderEmailsFetchedMsg{
		FolderName: "INBOX",
		Emails:     []fetcher.Email{{UID: 3, AccountID: "a"}, {UID: 4, AccountID: "b"}},
	})
	if strings.Contains(title(), "could not load") || strings.Contains(title(), "refreshing") {
		t.Fatalf("successful refresh kept the failure note or the refreshing marker:\n%s", title())
	}
}

func TestBrowserSignInSwitchesAccountToMatchaTokens(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{Accounts: []config.Account{
		{ID: "a", Email: "a@example.com", AuthMethod: "oauth2", OAuthTokenCmd: "thunderbird-token a@example.com"},
	}}
	settings := tui.NewSettings(cfg)
	store := emailstore.NewStore()
	m := &Model{
		current: settings, config: cfg, store: store, palette: palette.New(),
		actionManager: emailactions.NewManager(emailactions.Dependencies{Config: cfg}, store, nil),
		pluginBridge:  pluginbridge.NewManager(nil, store, cfg, nil),
	}

	m.Update(tui.OAuth2CompleteMsg{AccountID: "a", Email: "a@example.com", Err: errors.New("cancelled"), ReturnToSettings: true})
	if cfg.Accounts[0].OAuthTokenCmd == "" {
		t.Fatal("failed sign-in dropped the account's token command")
	}

	m.Update(tui.OAuth2CompleteMsg{AccountID: "a", Email: "a@example.com", ReturnToSettings: true})
	if cfg.Accounts[0].OAuthTokenCmd != "" {
		t.Fatal("successful sign-in kept using the external token command")
	}
	saved, err := config.LoadConfig()
	if err != nil || saved.Accounts[0].OAuthTokenCmd != "" {
		t.Fatalf("cleared token command was not saved (error %v)", err)
	}
	if m.current != settings {
		t.Fatal("sign-in started from settings did not return to settings")
	}
}

func TestEditingAccountKeepsSettingsTheFormDoesNotShow(t *testing.T) {
	existing := config.Account{
		ID: "a", Email: "me@outlook.com", ServiceProvider: "outlook", AuthMethod: "oauth2",
		OAuthTokenCmd: "token-cmd", PassCmd: "pass-cmd", PGPKeySource: "file", PGPPublicKey: "pub.asc",
		Password: "kept", ExtraAddresses: []string{"old@outlook.com", "removed@example.com"},
		IgnoredAddresses: []string{"dismissed@example.com"},
	}
	edited := buildAccount("a", tui.Credentials{
		Provider: "outlook", Host: "me@outlook.com", AuthMethod: "oauth2",
		ExtraAddresses:      []string{"old@outlook.com"},
		AlwaysShowMailFrom:  []string{"prof@example.com"},
		ShowUnaddressedMail: true, DiscoverExtraAddresses: true,
	}, "me@outlook.com")

	got := withEditedFields(existing, edited)
	if got.OAuthTokenCmd != "token-cmd" || got.PassCmd != "pass-cmd" || got.PGPKeySource != "file" || got.PGPPublicKey != "pub.asc" || got.Password != "kept" {
		t.Fatalf("editing lost settings the form does not show: %+v", got)
	}
	if !slices.Equal(got.ExtraAddresses, []string{"old@outlook.com"}) {
		t.Fatalf("ExtraAddresses = %v, want the edited list", got.ExtraAddresses)
	}
	if !slices.Equal(got.IgnoredAddresses, []string{"dismissed@example.com", "removed@example.com"}) {
		t.Fatalf("IgnoredAddresses = %v, want removed address added once", got.IgnoredAddresses)
	}
	readded := withEditedFields(got, config.Account{ExtraAddresses: []string{"old@outlook.com", "REMOVED@example.com"}})
	if !slices.Equal(readded.IgnoredAddresses, []string{"dismissed@example.com"}) {
		t.Fatalf("adding the address back did not remove it from IgnoredAddresses: %v", readded.IgnoredAddresses)
	}
	if !got.ShowUnaddressedMail || !got.DiscoverExtraAddresses || !slices.Equal(got.AlwaysShowMailFrom, []string{"prof@example.com"}) {
		t.Fatalf("edited address settings were not kept: %+v", got)
	}
}

func TestReplyFromOverrideUsesAliasAndKeepsDefaultForMainAddress(t *testing.T) {
	cfg := &config.Config{Accounts: []config.Account{{
		ID: "a", FetchEmail: "main@example.com", Name: "Teacher",
		ExtraAddresses: []string{"course@example.com"},
	}}}
	tests := []struct {
		name  string
		email fetcher.Email
		want  string
	}{
		{"alias in To", fetcher.Email{To: []string{"COURSE@example.com"}}, "Teacher <course@example.com>"},
		{"alias in Cc", fetcher.Email{To: []string{"other@example.net", "COURSE@example.com"}}, "Teacher <course@example.com>"},
		{"primary in Cc after alias", fetcher.Email{To: []string{"course@example.com", "main@example.com"}}, ""},
		{"alias only in delivery headers", fetcher.Email{To: []string{"other@example.net"}, DeliveryRecipients: []string{"COURSE@example.com"}}, "Teacher <course@example.com>"},
		{"primary in delivery headers beats To alias", fetcher.Email{To: []string{"course@example.com"}, DeliveryRecipients: []string{"main@example.com"}}, ""},
		{"primary in Cc beats delivery alias", fetcher.Email{To: []string{"other@example.net", "main@example.com"}, DeliveryRecipients: []string{"course@example.com"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.email.AccountID = "a"
			if got := replyFromOverride(cfg, tt.email); got != tt.want {
				t.Fatalf("From override = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReplyFromOverrideKeepsCatchAllDeliveryAddress(t *testing.T) {
	cfg := &config.Config{Accounts: []config.Account{{
		ID: "a", FetchEmail: "catch@example.com", Name: "User", CatchAll: true,
	}}}
	if got := replyFromOverride(cfg, fetcher.Email{AccountID: "a", To: []string{"specific@example.net"}}); got != "User <specific@example.net>" {
		t.Fatalf("catch-all From = %q", got)
	}
	email := fetcher.Email{
		AccountID: "a", To: []string{"unrelated@example.org"},
		DeliveryRecipients: []string{"specific@example.net", "forwarded@example.org"},
	}
	if got := replyFromOverride(cfg, email); got != "User <specific@example.net>" {
		t.Fatalf("catch-all From ignored actual delivery address: %q", got)
	}
	email.DeliveryRecipients = []string{"specific@example.net", "catch@example.com"}
	if got := replyFromOverride(cfg, email); got != "" {
		t.Fatalf("catch-all From overrode primary delivery recipient: %q", got)
	}
}

func TestCachedReplyRecipientsSurviveOpeningBody(t *testing.T) {
	cfg := &config.Config{Accounts: []config.Account{{
		ID: "a", FetchEmail: "main@example.com", ExtraAddresses: []string{"course@example.com"},
	}}}
	email := fetcher.Email{
		UID: 7, AccountID: "a", To: []string{"unrelated@example.net"},
		DeliveryRecipients: []string{"course@example.com"},
	}
	restored := emailstore.CacheToEmails(emailstore.EmailsToCache([]fetcher.Email{email}))
	store := emailstore.NewStore()
	store.SetFolder("INBOX", restored)
	store.UpdateEmailBodyByUID(email.UID, email.AccountID, "body", "text/plain", nil)
	opened := store.GetEmailByUIDAndAccount(email.UID, email.AccountID)
	if opened == nil || opened.Body != "body" {
		t.Fatalf("email body not opened: %+v", opened)
	}
	if got := replyFromOverride(cfg, *opened); got != "course@example.com" {
		t.Fatalf("opening cached body lost delivery alias: From override = %q", got)
	}
}
