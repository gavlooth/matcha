package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/floatpane/matcha/backend"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
)

// inboxWithSearchHistory returns a folder inbox whose search box saves
// searches in a temporary home. saved is listed newest first.
func inboxWithSearchHistory(t *testing.T, saved ...string) (*FolderInbox, config.SearchHistoryFile) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var history config.SearchHistoryFile
	for i := len(saved) - 1; i >= 0; i-- {
		if _, err := history.Add(saved[i]); err != nil {
			t.Fatal(err)
		}
	}
	accounts := []config.Account{{ID: "a", FetchEmail: "a@example.com"}}
	fi := NewFolderInbox([]string{keyINBOX}, accounts)
	fi.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
	fi.inbox.SetLocalSearcher(&fakeLocalSearcher{emails: []fetcher.Email{
		{UID: 1, AccountID: "a", Subject: "Invoice March"},
		{UID: 2, AccountID: "a", Subject: "Invoice April"},
		{UID: 3, AccountID: "a", Subject: "Holiday"},
	}})
	fi.inbox.SetSearchHistory(history)
	return fi, history
}

func savedSearches(t *testing.T, history config.SearchHistoryFile) []string {
	t.Helper()
	searches, err := history.Searches()
	if err != nil {
		t.Fatal(err)
	}
	return searches
}

func openSearch(t *testing.T, fi *FolderInbox) *SearchOverlay {
	t.Helper()
	fi.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if fi.inbox.searchOverlay == nil {
		t.Fatal("/ did not open the search box")
	}
	return fi.inbox.searchOverlay
}

func TestSearchesSavedWhenServerSearchedOrLocalMatchOpened(t *testing.T) {
	fi, history := inboxWithSearchHistory(t)
	openSearch(t, fi)
	typeText(fi, "invoice")
	fi.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd := fi.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := runUntilView(fi, cmd); !ok {
		t.Fatal("Enter on a local match did not open it")
	}
	if got := savedSearches(t, history); !slices.Equal(got, []string{"invoice"}) {
		t.Fatalf("after opening a local match, saved %q", got)
	}

	openSearch(t, fi)
	typeText(fi, "body:holiday")
	fi.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	fi.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := savedSearches(t, history); !slices.Equal(got, []string{"body:holiday", "invoice"}) {
		t.Fatalf("after searching the server, saved %q", got)
	}

	overlay := openSearch(t, fi)
	view := ansi.Strip(overlay.View())
	first, second := strings.Index(view, "> body:holiday"), strings.Index(view, "  invoice")
	if !strings.Contains(view, "Recent searches") || first < 0 || second < first {
		t.Fatalf("recent searches not listed newest first under the empty query:\n%s", view)
	}
}

func TestRecentSearchPickedWithKeysFillsQuery(t *testing.T) {
	fi, history := inboxWithSearchHistory(t, "holiday", "invoice")
	overlay := openSearch(t, fi)
	fi.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	fi.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	fi.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := overlay.input.Value(); got != "invoice" {
		t.Fatalf("Enter on the second recent search put %q in the query", got)
	}
	if overlay.resultsFocused || len(overlay.results) != 2 || overlay.fromServer {
		t.Fatalf("picking a recent search: focused %t, %d results, from server %t; want the query focused with 2 local matches",
			overlay.resultsFocused, len(overlay.results), overlay.fromServer)
	}

	// A second Enter searches the server for the picked query.
	_, cmd := fi.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	var asked backend.SearchQuery
	for _, msg := range collectMsgs(cmd) {
		if req, ok := msg.(SearchRequestedMsg); ok {
			asked = req.Query
		}
	}
	if asked.Raw != "invoice" {
		t.Fatalf("second Enter asked the server for %q", asked.Raw)
	}
	if got := savedSearches(t, history); !slices.Equal(got, []string{"invoice", "holiday"}) {
		t.Fatalf("searching again did not move the query to the top: %q", got)
	}
}

func TestTypingWhileRecentSearchesFocusedEditsQuery(t *testing.T) {
	fi, _ := inboxWithSearchHistory(t, "holiday")
	overlay := openSearch(t, fi)
	fi.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	typeText(fi, "x")
	if got := overlay.input.Value(); got != "x" || overlay.resultsFocused {
		t.Fatalf("typing on the recent searches: query %q, list focused %t", got, overlay.resultsFocused)
	}
}

func TestRecentSearchesRemovedAndCleared(t *testing.T) {
	fi, history := inboxWithSearchHistory(t, "a", "b", "c", "d")
	overlay := openSearch(t, fi)
	fi.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	fi.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	fi.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if got := savedSearches(t, history); !slices.Equal(got, []string{"a", "c", "d"}) {
		t.Fatalf("d on b left %q", got)
	}
	// The highlight stays on the row, now c; on the last row it moves up.
	fi.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	fi.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if got := savedSearches(t, history); !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("Delete on d left %q", got)
	}
	if view := ansi.Strip(overlay.View()); !strings.Contains(view, "> c") {
		t.Fatalf("highlight not moved to the last remaining search:\n%s", view)
	}
	if got := overlay.input.Value(); got != "" {
		t.Fatalf("removing searches typed %q into the query", got)
	}

	fi.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})
	if got := savedSearches(t, history); len(got) != 0 {
		t.Fatalf("D left %q", got)
	}
	if overlay.resultsFocused || strings.Contains(ansi.Strip(overlay.View()), "Recent searches") {
		t.Fatal("after clearing, the recent list is still shown or focused")
	}
	typeText(fi, "d")
	if got := overlay.input.Value(); got != "d" {
		t.Fatalf("after clearing, typing d gave query %q", got)
	}
}

func TestClickingRecentSearchAfterScrollingPicksIt(t *testing.T) {
	saved := make([]string, 12)
	for i := range saved {
		saved[i] = fmt.Sprintf("query %02d", i+1)
	}
	fi, _ := inboxWithSearchHistory(t, saved...)
	overlay := openSearch(t, fi)
	// ↑ wraps to the last search, scrolling the first two out of view.
	fi.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	rows := strings.Split(ansi.Strip(fi.View().Content), "\n")
	row := -1
	for y, line := range rows {
		if strings.Contains(line, "query 05") {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatalf("query 05 not drawn:\n%s", strings.Join(rows, "\n"))
	}
	click := tea.MouseClickMsg{X: 60, Y: row, Button: tea.MouseLeft}
	fi.Update(click)
	if overlay.cursor != 4 || overlay.input.Value() != "" {
		t.Fatalf("click on query 05 highlighted row %d, query %q", overlay.cursor+1, overlay.input.Value())
	}
	fi.Update(click)
	if got := overlay.input.Value(); got != "query 05" || overlay.resultsFocused {
		t.Fatalf("second click put %q in the query, list focused %t", got, overlay.resultsFocused)
	}
}
