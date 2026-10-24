package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSearchHistoryKeepsNewestFirstWithoutRepeats(t *testing.T) {
	isolatedMailRulesHome(t)
	var history SearchHistoryFile
	for _, query := range []string{"invoice", "  from:alice  ", "", "   ", "invoice"} {
		if _, err := history.Add(query); err != nil {
			t.Fatal(err)
		}
	}
	got, err := history.Searches()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"invoice", "from:alice"}; !slices.Equal(got, want) {
		t.Fatalf("saved searches %q, want %q", got, want)
	}
}

func TestSearchHistoryDropsOldestBeyondFifty(t *testing.T) {
	isolatedMailRulesHome(t)
	var history SearchHistoryFile
	var got []string
	for i := 1; i <= 52; i++ {
		var err error
		if got, err = history.Add(fmt.Sprintf("query %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 50 || got[0] != "query 52" || got[49] != "query 3" || slices.Contains(got, "query 2") {
		t.Fatalf("got %d searches, newest %q, oldest %q", len(got), got[0], got[len(got)-1])
	}
	// Searching a saved query again moves it to the top without dropping another.
	got, err := history.Add("query 10")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 || got[0] != "query 10" || got[1] != "query 52" || got[49] != "query 3" {
		t.Fatalf("got %d searches, first %q, second %q, last %q", len(got), got[0], got[1], got[len(got)-1])
	}
}

func TestSearchHistoryRemoveAndClear(t *testing.T) {
	isolatedMailRulesHome(t)
	var history SearchHistoryFile
	for _, query := range []string{"a", "b", "c"} {
		if _, err := history.Add(query); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := history.Remove("b"); err != nil || !slices.Equal(got, []string{"c", "a"}) {
		t.Fatalf("after removing b: %q %v", got, err)
	}
	if got, _ := history.Searches(); !slices.Equal(got, []string{"c", "a"}) {
		t.Fatalf("removal not saved: %q", got)
	}
	if err := history.Clear(); err != nil {
		t.Fatal(err)
	}
	if got, err := history.Searches(); err != nil || len(got) != 0 {
		t.Fatalf("after clearing: %q %v", got, err)
	}
}

func TestSearchHistoryFollowsEncryptionSetting(t *testing.T) {
	home := isolatedMailRulesHome(t)
	var history SearchHistoryFile
	if _, err := history.Add("subject:private"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".cache", "matcha", "search_history.json")
	if err := EnableSecureMode("isolated-test-passphrase", nil); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || json.Valid(data) || bytes.Contains(data, []byte("private")) {
		t.Fatalf("enabling encryption left the search history readable: %v", err)
	}
	if _, err := history.Add("from:bob"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || bytes.Contains(data, []byte("bob")) {
		t.Fatalf("search saved while encrypted was written readable: %v", err)
	}
	if err := DisableSecureMode(nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	var got []string
	if err != nil || json.Unmarshal(data, &got) != nil || !slices.Equal(got, []string{"from:bob", "subject:private"}) {
		t.Fatalf("disabling encryption did not restore the history: %q %v", data, err)
	}
}
