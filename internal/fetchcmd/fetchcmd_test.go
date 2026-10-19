package fetchcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/tui"
)

func TestFetchFolderEmailsCmdUsesPageLimit(t *testing.T) {
	root := t.TempDir()
	for _, subdir := range []string{"cur", "new", "tmp"} {
		if err := os.Mkdir(filepath.Join(root, subdir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 3 {
		message := fmt.Sprintf("From: sender@example.com\r\nTo: me@example.com\r\nSubject: message %d\r\nMessage-ID: <message-%d@example.com>\r\n\r\nbody\r\n", i, i)
		if err := os.WriteFile(filepath.Join(root, "new", fmt.Sprintf("message-%d", i)), []byte(message), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Accounts: []config.Account{{ID: "maildir", Protocol: "maildir", MaildirPath: root}}}
	msg := FetchFolderEmailsCmd(cfg, "INBOX", 2)()
	fetched, ok := msg.(tui.FolderEmailsFetchedMsg)
	if !ok || len(fetched.Emails) != 2 {
		t.Fatalf("first page = %#v, want two messages", msg)
	}
}

func TestFolderPageForAccountWithoutTheFolderIsEmptyNotAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := config.SaveFolderCache(&config.FolderCache{Accounts: []config.CachedFolders{
		{AccountID: "gmail", Folders: []string{"INBOX", "Important to Review"}},
		{AccountID: "outlook", Folders: []string{"INBOX"}},
	}}); err != nil {
		t.Fatal(err)
	}
	// The maildir does not exist, so any real request would fail.
	outlook := &config.Account{ID: "outlook", Email: "o@example.com", Protocol: "maildir", MaildirPath: "/nonexistent"}
	msg := FetchFolderEmailsPaginatedCmd(outlook, "Important to Review", 40, 40)()
	page, ok := msg.(tui.FolderEmailsAppendedMsg)
	if !ok || page.AccountID != "outlook" || len(page.Emails) != 0 {
		t.Fatalf("page for an account without the folder = %#v, want an empty page", msg)
	}
	if _, failed := FetchFolderEmailsPaginatedCmd(outlook, "INBOX", 40, 40)().(tui.FolderEmailsPageFailedMsg); !failed {
		t.Fatal("a folder the account has should still be requested and report its failure")
	}
}
