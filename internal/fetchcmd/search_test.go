package fetchcmd

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/backend"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/tui"
)

// fakeSearchProvider implements only Search; the embedded nil Provider makes
// any other call panic, which the tests do not expect.
type fakeSearchProvider struct {
	backend.Provider
	search func(ctx context.Context, folder string, query backend.SearchQuery) ([]backend.Email, error)
}

func (f *fakeSearchProvider) Search(ctx context.Context, folder string, query backend.SearchQuery) ([]backend.Email, error) {
	return f.search(ctx, folder, query)
}

func searchConfig(ids ...string) *config.Config {
	cfg := &config.Config{}
	for _, id := range ids {
		cfg.Accounts = append(cfg.Accounts, config.Account{ID: id, Email: id + "@example.com"})
	}
	return cfg
}

func resolverFor(providers map[string]backend.Provider) ProviderResolver {
	return func(acc *config.Account) backend.Provider {
		if p, ok := providers[acc.ID]; ok {
			return p
		}
		return nil
	}
}

func runSearch(t *testing.T, cmd tea.Cmd) tui.SearchResultsMsg {
	t.Helper()
	raw := cmd()
	msg, ok := raw.(tui.SearchResultsMsg)
	if !ok {
		t.Fatalf("message type = %T, want tui.SearchResultsMsg", raw)
	}
	return msg
}

func TestSearchEmailsCmdSearchesAccountsConcurrently(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	query := backend.ParseSearchQuery("invoice")

	// Every provider waits until all of them have started. A search that
	// runs the accounts one after another never gets past the first wait.
	var started sync.WaitGroup
	started.Add(len(ids))
	allStarted := make(chan struct{})
	go func() {
		started.Wait()
		close(allStarted)
	}()
	waitForAll := func(ctx context.Context) error {
		started.Done()
		select {
		case <-allStarted:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return errors.New("accounts were searched one after another")
		}
	}

	results := map[string][]backend.Email{
		"a": {{UID: 1, Subject: "a-old", Date: base, AccountID: "a"}},
		"d": {
			{UID: 7, Subject: "d-new", Date: base.Add(2 * time.Hour), AccountID: "d"},
			{UID: 5, Subject: "d-mid", Date: base.Add(time.Hour), AccountID: "d"},
		},
	}
	errs := map[string]error{
		"b": backend.ErrNotSupported,
		"c": errors.New("server down"),
	}

	providers := map[string]backend.Provider{}
	for _, id := range ids {
		providers[id] = &fakeSearchProvider{search: func(ctx context.Context, folder string, q backend.SearchQuery) ([]backend.Email, error) {
			if folder != "INBOX" || q.Text != "invoice" {
				t.Errorf("account %s searched folder %q text %q", id, folder, q.Text)
			}
			if err := waitForAll(ctx); err != nil {
				return nil, err
			}
			return results[id], errs[id]
		}}
	}

	msg := runSearch(t, SearchEmailsCmd(searchConfig(ids...), resolverFor(providers), query, "INBOX", ""))

	if msg.Err != nil {
		t.Fatalf("Err = %v, want nil because some accounts succeeded", msg.Err)
	}
	var subjects []string
	for _, e := range msg.Emails {
		subjects = append(subjects, e.Subject)
	}
	want := []string{"d-new", "d-mid", "a-old"}
	if len(subjects) != len(want) {
		t.Fatalf("subjects = %v, want %v", subjects, want)
	}
	for i := range want {
		if subjects[i] != want[i] {
			t.Fatalf("subjects = %v, want %v (newest first)", subjects, want)
		}
	}
	if msg.Query.Raw != query.Raw {
		t.Fatalf("Query.Raw = %q, want %q", msg.Query.Raw, query.Raw)
	}
}

func TestSearchEmailsCmdReportsFirstAccountErrorWhenAllFail(t *testing.T) {
	errA := errors.New("account a failed")
	errB := errors.New("account b failed")
	bReturned := make(chan struct{})

	providers := map[string]backend.Provider{
		// a fails only after b has already failed, so the reported error
		// must follow account order, not which goroutine finished first.
		"a": &fakeSearchProvider{search: func(ctx context.Context, _ string, _ backend.SearchQuery) ([]backend.Email, error) {
			select {
			case <-bReturned:
			case <-ctx.Done():
			}
			return nil, errA
		}},
		"b": &fakeSearchProvider{search: func(context.Context, string, backend.SearchQuery) ([]backend.Email, error) {
			defer close(bReturned)
			return nil, errB
		}},
		"c": &fakeSearchProvider{search: func(context.Context, string, backend.SearchQuery) ([]backend.Email, error) {
			return nil, backend.ErrNotSupported
		}},
	}

	msg := runSearch(t, SearchEmailsCmd(searchConfig("a", "b", "c"), resolverFor(providers), backend.ParseSearchQuery("x"), "INBOX", ""))
	if !errors.Is(msg.Err, errA) {
		t.Fatalf("Err = %v, want %v", msg.Err, errA)
	}
	if len(msg.Emails) != 0 {
		t.Fatalf("Emails = %v, want none", msg.Emails)
	}
}

func TestSearchEmailsCmdMissingProviderIsAnError(t *testing.T) {
	msg := runSearch(t, SearchEmailsCmd(searchConfig("a"), resolverFor(nil), backend.ParseSearchQuery("x"), "INBOX", ""))
	if msg.Err == nil {
		t.Fatal("Err = nil, want an error for an account without a provider")
	}
}

func TestSearchEmailsCmdSkipsUnsupportedAccounts(t *testing.T) {
	unsupported := &fakeSearchProvider{search: func(context.Context, string, backend.SearchQuery) ([]backend.Email, error) {
		return nil, backend.ErrNotSupported
	}}
	providers := map[string]backend.Provider{"a": unsupported, "b": unsupported}

	msg := runSearch(t, SearchEmailsCmd(searchConfig("a", "b"), resolverFor(providers), backend.ParseSearchQuery("x"), "INBOX", ""))
	if msg.Err != nil || len(msg.Emails) != 0 {
		t.Fatalf("msg = %+v, want no error and no emails", msg)
	}
}

func TestSearchEmailsCmdOnlySearchesSelectedAccount(t *testing.T) {
	var mu sync.Mutex
	var searched []string
	providers := map[string]backend.Provider{}
	for _, id := range []string{"a", "b"} {
		providers[id] = &fakeSearchProvider{search: func(context.Context, string, backend.SearchQuery) ([]backend.Email, error) {
			mu.Lock()
			searched = append(searched, id)
			mu.Unlock()
			return []backend.Email{{UID: 1, AccountID: id}}, nil
		}}
	}

	msg := runSearch(t, SearchEmailsCmd(searchConfig("a", "b"), resolverFor(providers), backend.ParseSearchQuery("x"), "INBOX", "b"))
	if msg.Err != nil || len(msg.Emails) != 1 || msg.Emails[0].AccountID != "b" {
		t.Fatalf("msg = %+v, want one email from account b", msg)
	}
	if len(searched) != 1 || searched[0] != "b" {
		t.Fatalf("searched accounts = %v, want [b]", searched)
	}
}
