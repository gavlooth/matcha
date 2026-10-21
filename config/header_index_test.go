package config

import (
	"testing"
)

func TestHeaderIndexSaveLoadAndAccountCleanup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	empty, err := LoadHeaderIndex("acct-a", "INBOX")
	if err != nil || len(empty.Emails) != 0 {
		t.Fatalf("a folder never indexed should load empty without error, got %+v, %v", empty, err)
	}

	saved := &HeaderIndex{UIDValidity: 7, Emails: []CachedEmail{{UID: 3, Subject: "hello"}}, OtherUIDs: []uint32{9}}
	if err := SaveHeaderIndex("acct-a", "Archive/2016", saved); err != nil {
		t.Fatal(err)
	}
	if err := SaveHeaderIndex("acct-b", "INBOX", &HeaderIndex{UIDValidity: 1}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadHeaderIndex("acct-a", "Archive/2016")
	if err != nil || loaded.UIDValidity != 7 || len(loaded.Emails) != 1 || loaded.Emails[0].Subject != "hello" || len(loaded.OtherUIDs) != 1 {
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
