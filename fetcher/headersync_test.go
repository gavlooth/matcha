package fetcher

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/floatpane/matcha/config"
)

// memIMAP starts an in-memory IMAP server with one user and returns a
// logged-in client.
func memIMAP(t *testing.T) *imapclient.Client {
	t.Helper()
	mem := imapmemserver.New()
	user := imapmemserver.NewUser("me@example.com", "secret")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	mem.AddUser(user)
	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}},
		InsecureAuth: true,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(ln)                  //nolint:errcheck
	t.Cleanup(func() { server.Close() }) //nolint:errcheck
	c, err := imapclient.DialInsecure(ln.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() }) //nolint:errcheck
	if err := c.Login("me@example.com", "secret").Wait(); err != nil {
		t.Fatal(err)
	}
	return c
}

func appendMessage(t *testing.T, c *imapclient.Client, folder, to, subject string, flags ...imap.Flag) {
	t.Helper()
	raw := fmt.Sprintf("From: Sender <sender@example.com>\r\nTo: %s\r\nSubject: %s\r\nDate: Mon, 19 Oct 2026 10:00:00 +0000\r\nMessage-ID: <%s@example.com>\r\n\r\nbody\r\n", to, subject, subject)
	cmd := c.Append(folder, int64(len(raw)), &imap.AppendOptions{Flags: flags})
	if _, err := cmd.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func headerUIDs(h FolderHeaders) []uint32 {
	var uids []uint32
	for _, e := range h.Emails {
		uids = append(uids, e.UID)
	}
	slices.Sort(uids)
	return uids
}

func TestSyncFolderHeadersFetchesNewestFirstInSteps(t *testing.T) {
	c := memIMAP(t)
	account := &config.Account{ID: "a", Email: "me@example.com"}
	for i := 1; i <= 5; i++ {
		appendMessage(t, c, "INBOX", "me@example.com", fmt.Sprintf("mail%d", i))
	}

	headers, missing, err := scanFolderHeaders(c, account, "INBOX", FolderHeaders{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := headerUIDs(headers); !slices.Equal(got, []uint32{3, 4, 5}) || !slices.Equal(missing, []uint32{2, 1}) {
		t.Fatalf("scan fetched UIDs %v leaving %v, want the newest [3 4 5] leaving [2 1]", got, missing)
	}
	headers, missing, err = continueFolderHeaders(c, account, "INBOX", headers, missing, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := headerUIDs(headers); !slices.Equal(got, []uint32{1, 2, 3, 4, 5}) || len(missing) != 0 {
		t.Fatalf("next step has UIDs %v leaving %v, want all 5 and none left", got, missing)
	}
	if _, _, err := continueFolderHeaders(c, account, "INBOX", FolderHeaders{UIDValidity: headers.UIDValidity + 1}, []uint32{1}, 3); !errors.Is(err, ErrFolderUIDsChanged) {
		t.Fatalf("continuing after the folder's UIDVALIDITY changed returned %v", err)
	}
}

func TestSyncFolderHeadersDropsDeletedMailAndUpdatesReadState(t *testing.T) {
	c := memIMAP(t)
	account := &config.Account{ID: "a", Email: "me@example.com"}
	for i := 1; i <= 3; i++ {
		appendMessage(t, c, "INBOX", "me@example.com", fmt.Sprintf("mail%d", i))
	}
	headers, _, err := scanFolderHeaders(c, account, "INBOX", FolderHeaders{}, 10)
	if err != nil {
		t.Fatal(err)
	}

	// scanFolderHeaders opened the folder read-only; select it for writing.
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	seen := imap.UIDSetNum(1)
	if err := c.Store(seen, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	gone := imap.UIDSetNum(2)
	if err := c.Store(gone, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Expunge().Close(); err != nil {
		t.Fatal(err)
	}

	headers, _, err = scanFolderHeaders(c, account, "INBOX", headers, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := headerUIDs(headers); !slices.Equal(got, []uint32{1, 3}) {
		t.Fatalf("after deleting UID 2 the index has %v", got)
	}
	for _, e := range headers.Emails {
		if e.UID == 1 && !e.IsRead {
			t.Fatal("UID 1 was marked read on the server but the index still says unread")
		}
	}
}

func TestSyncFolderHeadersStartsOverWhenUIDValidityChanges(t *testing.T) {
	c := memIMAP(t)
	account := &config.Account{ID: "a", Email: "me@example.com"}
	appendMessage(t, c, "INBOX", "me@example.com", "mail1")
	stale := FolderHeaders{UIDValidity: 999999, Emails: []Email{{UID: 1, Subject: "from an old UIDVALIDITY"}}}

	headers, _, err := scanFolderHeaders(c, account, "INBOX", stale, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(headers.Emails) != 1 || headers.Emails[0].Subject != "mail1" {
		t.Fatalf("index kept headers from another UIDVALIDITY: %+v", headers.Emails)
	}
}

func TestSyncFolderHeadersRemembersMailForOtherAccounts(t *testing.T) {
	c := memIMAP(t)
	account := &config.Account{ID: "a", Email: "me@example.com"}
	appendMessage(t, c, "INBOX", "me@example.com", "mine")
	appendMessage(t, c, "INBOX", "someone-else@example.com", "theirs")

	headers, _, err := scanFolderHeaders(c, account, "INBOX", FolderHeaders{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := headerUIDs(headers); !slices.Equal(got, []uint32{1}) || !slices.Equal(headers.OtherUIDs, []uint32{2}) {
		t.Fatalf("emails %v, other UIDs %v; want [1] and [2]", got, headers.OtherUIDs)
	}
	headers, missing, err := scanFolderHeaders(c, account, "INBOX", headers, 10)
	if err != nil || len(missing) != 0 || len(headers.Emails) != 1 {
		t.Fatalf("second scan: emails %d, missing %v, err %v; the other account's mail must not be fetched again", len(headers.Emails), missing, err)
	}
}
