package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/floatpane/matcha/backend"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
)

// searchWithResults opens the search box in a folder inbox and delivers n results.
func searchWithResults(t *testing.T, n int) *FolderInbox {
	t.Helper()
	accounts := []config.Account{{ID: "a", FetchEmail: "a@example.com"}}
	inboxEmails := []fetcher.Email{{UID: 900, AccountID: "a", From: "x@example.com", Subject: "Not a result"}}
	fi := NewFolderInbox([]string{keyINBOX}, accounts)
	fi.SetEmails(inboxEmails, accounts)
	fi.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
	fi.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if fi.inbox.searchOverlay == nil {
		t.Fatal("/ did not open the search box")
	}
	results := make([]fetcher.Email, n)
	for i := range results {
		results[i] = fetcher.Email{UID: uint32(i + 1), AccountID: "a", From: "s@example.com", Subject: fmt.Sprintf("Result%02d", i+1)}
	}
	typeText(fi, "result")
	fi.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	fi.Update(SearchResultsMsg{Query: backend.ParseSearchQuery("result"), Emails: results})
	return fi
}

func typeText(fi *FolderInbox, text string) {
	for _, r := range text {
		fi.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// fakeLocalSearcher matches emails whose subject contains the bare words.
type fakeLocalSearcher struct {
	emails  []fetcher.Email
	folders []string
}

func (f *fakeLocalSearcher) CanSearch(q backend.SearchQuery) bool {
	return q.Text != "" && q.Body == ""
}
func (f *fakeLocalSearcher) Search(q backend.SearchQuery, folder string, _ []string, _ int) []fetcher.Email {
	f.folders = append(f.folders, folder)
	var out []fetcher.Email
	for _, e := range f.emails {
		if strings.Contains(strings.ToLower(e.Subject), strings.ToLower(q.Text)) {
			out = append(out, e)
		}
	}
	return out
}
func (f *fakeLocalSearcher) Count(string, []string) int    { return len(f.emails) }
func (f *fakeLocalSearcher) Syncing(string, []string) bool { return false }

func TestTypingShowsLocalMatchesAndEnterStillAsksTheServer(t *testing.T) {
	accounts := []config.Account{{ID: "a", FetchEmail: "a@example.com"}}
	fi := NewFolderInbox([]string{keyINBOX, "Archive"}, accounts)
	fi.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
	local := &fakeLocalSearcher{emails: []fetcher.Email{
		{UID: 1, AccountID: "a", Subject: "Invoice March"},
		{UID: 2, AccountID: "a", Subject: "Invoice April"},
		{UID: 3, AccountID: "a", Subject: "Holiday"},
	}}
	fi.inbox.SetLocalSearcher(local)
	_, cmd := fi.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	var opened bool
	for _, msg := range collectMsgs(cmd) {
		if o, ok := msg.(SearchOpenedMsg); ok && o.FolderName == keyINBOX {
			opened = true
		}
	}
	if !opened {
		t.Fatal("opening the search box did not ask the app to update the INBOX copy")
	}

	typeText(fi, "invoice")
	overlay := fi.inbox.searchOverlay
	if len(overlay.results) != 2 || overlay.fromServer || overlay.resultsFocused {
		t.Fatalf("typing: %d results, from server %t, results focused %t; want 2 local matches with the query still focused", len(overlay.results), overlay.fromServer, overlay.resultsFocused)
	}
	if view := ansi.Strip(overlay.View()); !strings.Contains(view, "2 match(es) on this computer") {
		t.Fatalf("local match count not shown:\n%s", view)
	}

	// ↓ lands on the first match, Enter opens it.
	fi.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd = fi.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view, ok := runUntilView(fi, cmd); !ok || view.UID != 1 {
		t.Fatalf("Enter on the first local match opened %+v (found %t)", view, ok)
	}

	// Enter while the query is focused asks the server, and stale server
	// results for an older query are dropped.
	fi.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeText(fi, "holiday")
	_, cmd = fi.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	var asked bool
	for _, msg := range collectMsgs(cmd) {
		if _, ok := msg.(SearchRequestedMsg); ok {
			asked = true
		}
	}
	if !asked {
		t.Fatal("Enter in the query did not search the server")
	}
	fi.Update(SearchResultsMsg{Query: backend.ParseSearchQuery("holi"), Emails: []fetcher.Email{{UID: 50}}})
	if fi.inbox.searchOverlay.fromServer {
		t.Fatal("server results for an older query replaced the current ones")
	}
	fi.Update(SearchResultsMsg{Query: backend.ParseSearchQuery("holiday"), Emails: []fetcher.Email{{UID: 3, Subject: "Holiday"}, {UID: 60, Subject: "Holiday plans"}}})
	if o := fi.inbox.searchOverlay; !o.fromServer || len(o.results) != 2 || !o.resultsFocused {
		t.Fatalf("server results not shown and focused: from server %t, %d results", o.fromServer, len(o.results))
	}
}

// runUntilView runs cmd and feeds the messages it produces back into fi
// until one of them is a ViewEmailMsg.
func runUntilView(fi *FolderInbox, cmd tea.Cmd) (ViewEmailMsg, bool) {
	for _, msg := range collectMsgs(cmd) {
		if view, ok := msg.(ViewEmailMsg); ok {
			return view, true
		}
		_, next := fi.Update(msg)
		if view, ok := runUntilView(fi, next); ok {
			return view, true
		}
	}
	return ViewEmailMsg{}, false
}

func TestSearchResultPickedWithKeysOpens(t *testing.T) {
	fi := searchWithResults(t, 15)
	// j twice, k once, then ↓ ten times: past the 10 shown rows, so the window scrolls.
	for _, key := range []tea.KeyPressMsg{{Code: 'j', Text: "j"}, {Code: 'j', Text: "j"}, {Code: 'k', Text: "k"}} {
		fi.Update(key)
	}
	for range 10 {
		fi.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if got := fi.inbox.searchOverlay.input.Value(); got != "result" {
		t.Fatalf("j/k on the results were typed into the query: %q", got)
	}
	if view := ansi.Strip(fi.inbox.searchOverlay.View()); !strings.Contains(view, "> 12. ") {
		t.Fatalf("highlighted result 12 not drawn:\n%s", view)
	}
	_, cmd := fi.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view, ok := runUntilView(fi, cmd)
	if !ok || view.UID != 12 {
		t.Fatalf("Enter opened %+v (found %t), want UID 12", view, ok)
	}
	if fi.inbox.searchOverlay != nil || !fi.inbox.searchActive {
		t.Fatal("opening a result should close the box and keep the results in the list")
	}
	if it, _ := fi.inbox.list.SelectedItem().(item); it.uid != 12 {
		t.Fatalf("list cursor on UID %d after opening, want 12", it.uid)
	}
}

func TestSearchTabSwitchesBetweenQueryAndResults(t *testing.T) {
	fi := searchWithResults(t, 3)
	fi.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	fi.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := fi.inbox.searchOverlay.input.Value(); got != "resultj" {
		t.Fatalf("after Tab, j should edit the query, got %q", got)
	}
	fi.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	fi.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := fi.inbox.searchOverlay.cursor; got != 1 {
		t.Fatalf("after the second Tab, j should move to result 2, cursor %d", got)
	}
}

func TestClickingSearchResultPicksItAndLeavesListAlone(t *testing.T) {
	fi := searchWithResults(t, 5)
	before, _ := fi.inbox.list.SelectedItem().(item)
	rows := strings.Split(ansi.Strip(fi.View().Content), "\n")
	row := -1
	for y, line := range rows {
		if strings.Contains(line, "Result03") {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatal("Result03 not drawn")
	}
	click := tea.MouseClickMsg{X: 60, Y: row, Button: tea.MouseLeft}
	fi.Update(click)
	if got := fi.inbox.searchOverlay.cursor; got != 2 {
		t.Fatalf("click on Result03 highlighted result %d", got+1)
	}
	if after, _ := fi.inbox.list.SelectedItem().(item); after.uid != before.uid {
		t.Fatalf("click in the search box moved the list behind it to UID %d", after.uid)
	}
	_, cmd := fi.Update(click)
	if view, ok := runUntilView(fi, cmd); !ok || view.UID != 3 {
		t.Fatalf("double click opened %+v (found %t), want UID 3", view, ok)
	}
}
