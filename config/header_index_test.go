package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHeaderIndexSaveLoadAndAccountCleanup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	empty, err := LoadHeaderIndex("acct-a", "INBOX")
	if err != nil || len(empty.Emails) != 0 || empty.Format != currentHeaderIndexFormat {
		t.Fatalf("a folder never indexed should load empty without error, got %+v, %v", empty, err)
	}

	saved := &HeaderIndex{
		UIDValidity: 7, Emails: []CachedEmail{{UID: 3, Subject: "hello"}},
		HiddenEmails: []CachedEmail{{UID: 9, Subject: "hidden"}},
	}
	if err := SaveHeaderIndex("acct-a", "Archive/2016", saved); err != nil {
		t.Fatal(err)
	}
	if err := SaveHeaderIndex("acct-b", "INBOX", &HeaderIndex{UIDValidity: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadHeaderIndex("acct-a", "Archive/2016")
	if err != nil || loaded.UIDValidity != 7 || len(loaded.Emails) != 1 || loaded.Emails[0].Subject != "hello" || len(loaded.HiddenEmails) != 1 || loaded.HiddenEmails[0].UID != 9 || loaded.Format != currentHeaderIndexFormat {
		t.Fatalf("loaded %+v, %v", loaded, err)
	}

	if err := CleanupAccountCache("acct-a"); err != nil {
		t.Fatal(err)
	}
	if gone, _ := LoadHeaderIndex("acct-a", "Archive/2016"); len(gone.Emails) != 0 {
		t.Fatal("removing an account kept its header index")
	}
	if kept, _ := LoadHeaderIndex("acct-b", "INBOX"); kept.UIDValidity != 1 {
		t.Fatal("removing one account deleted another account's header index")
	}
}

func TestHeaderIndexOlderFormatStartsOver(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	path, err := headerIndexFile("acct-a", "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"format":2,"uid_validity":7,"emails":[{"uid":3}],"hidden_emails":[{"uid":9}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := LoadHeaderIndex("acct-a", "INBOX")
	if err != nil || index.Format != currentHeaderIndexFormat || index.UIDValidity != 0 || len(index.Emails) != 0 || len(index.HiddenEmails) != 0 {
		t.Fatalf("older index was not discarded: %+v, %v", index, err)
	}
}
