package headerindex

import (
	"slices"
	"testing"
	"time"

	"github.com/floatpane/matcha/backend"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
)

func indexWith(accountID, folder string, emails ...fetcher.Email) *Index {
	x := New()
	x.folders[folderKey{accountID, folder}] = newFolderIndex(fetcher.FolderHeaders{Emails: emails})
	return x
}

func subjects(emails []fetcher.Email) []string {
	var out []string
	for _, e := range emails {
		out = append(out, e.Subject)
	}
	return out
}

var day = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

var sample = []fetcher.Email{
	{UID: 1, From: "Wefunder <support@wefunder.com>", Subject: "Identity verification step required", Date: day},
	{UID: 2, From: "GitHub <noreply@github.com>", Subject: "[GitHub] Sudo email verification code", Date: day.AddDate(0, 0, 1)},
	{UID: 3, From: "Yahoo <info@yahoo.com>", Subject: "Προσεχής ενημέρωση για τις υπηρεσίες", Date: day.AddDate(0, 0, 2)},
	{UID: 4, From: "Shop <news@shop.example>", Subject: "Very nice offer for you, Fiona", Date: day.AddDate(0, 0, 3)},
	{UID: 5, From: "Anna <anna@example.com>", Subject: "Notes about github actions", Date: day.AddDate(0, 0, 4)},
	{UID: 6, From: "Café Lumière <hello@cafe.example>", Subject: "Menu", Date: day.AddDate(0, 0, 5)},
}

func search(t *testing.T, raw string) []string {
	t.Helper()
	return subjects(indexWith("a", "INBOX", sample...).Search(backend.ParseSearchQuery(raw), "INBOX", []string{"a"}, 50))
}

func TestPartialWordMatchesButScatteredLettersDoNot(t *testing.T) {
	got := search(t, "verif")
	want := []string{"[GitHub] Sudo email verification code", "Identity verification step required"}
	if !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want))) {
		t.Fatalf("verif matched %q, want the two verification mails and not %q", got, sample[3].Subject)
	}
}

func TestFuzzyMatchSkipsLetters(t *testing.T) {
	if got := search(t, "vrfctn"); len(got) != 2 {
		t.Fatalf("vrfctn matched %q, want both verification mails", got)
	}
}

func TestAccentsAndCaseAreIgnored(t *testing.T) {
	if got := search(t, "ΕΝΗΜΕΡΩΣΗ"); !slices.Equal(got, []string{sample[2].Subject}) {
		t.Fatalf("Greek without accents matched %q", got)
	}
	if got := search(t, "cafe"); !slices.Equal(got, []string{"Menu"}) {
		t.Fatalf("cafe matched %q, want the mail from Café Lumière", got)
	}
}

func TestEveryWordMustMatch(t *testing.T) {
	if got := search(t, "wefunder verif"); !slices.Equal(got, []string{sample[0].Subject}) {
		t.Fatalf("wefunder verif matched %q", got)
	}
}

func TestFieldPrefixLimitsWhereAWordMatches(t *testing.T) {
	if got := search(t, "from:github"); !slices.Equal(got, []string{sample[1].Subject}) {
		t.Fatalf("from:github matched %q, want only the mail GitHub sent", got)
	}
	if got := search(t, "github"); len(got) != 2 {
		t.Fatalf("bare github matched %q, want the GitHub mail and the subject that mentions it", got)
	}
}

func TestDatesFilterAndNewerWinsTies(t *testing.T) {
	got := search(t, "verification since:2026-10-02")
	if !slices.Equal(got, []string{sample[1].Subject}) {
		t.Fatalf("since:2026-10-02 matched %q", got)
	}
}

func TestBodyQueriesAreLeftToTheServer(t *testing.T) {
	x := indexWith("a", "INBOX", sample...)
	q := backend.ParseSearchQuery("body:verification")
	if x.CanSearch(q) || x.Search(q, "INBOX", []string{"a"}, 50) != nil {
		t.Fatal("a body: query must not be answered from headers")
	}
}

func TestSearchOnlyLooksAtTheGivenAccountsAndFolder(t *testing.T) {
	x := indexWith("a", "INBOX", sample...)
	x.folders[folderKey{"b", "INBOX"}] = newFolderIndex(fetcher.FolderHeaders{Emails: []fetcher.Email{{UID: 9, Subject: "verification for b"}}})
	x.folders[folderKey{"a", "Archive"}] = newFolderIndex(fetcher.FolderHeaders{Emails: []fetcher.Email{{UID: 8, Subject: "verification archived"}}})
	got := subjects(x.Search(backend.ParseSearchQuery("verification"), "INBOX", []string{"b"}, 50))
	if !slices.Equal(got, []string{"verification for b"}) {
		t.Fatalf("searching account b INBOX matched %q", got)
	}
	if n := x.Count("INBOX", []string{"a", "b"}); n != len(sample)+1 {
		t.Fatalf("Count = %d", n)
	}
}

func TestHiddenMailQueriesAndRecipientCounts(t *testing.T) {
	x := New()
	hidden := []fetcher.Email{
		{UID: 2, AccountID: "a", Subject: "hidden verification", To: []string{"me@example.com", "old@example.com", "z@example.com", "z@example.com", "a@example.com"}, Date: day.Add(time.Hour)},
		{UID: 3, AccountID: "a", Subject: "older hidden verification", To: []string{"z@example.com", "b@example.com"}, Date: day},
	}
	x.folders[folderKey{"a", "INBOX"}] = newFolderIndex(fetcher.FolderHeaders{
		Emails: []fetcher.Email{{UID: 1, Subject: "visible verification", Date: day}},
		Hidden: hidden,
	})
	x.folders[folderKey{"b", "INBOX"}] = newFolderIndex(fetcher.FolderHeaders{
		Hidden: []fetcher.Email{{UID: 9, AccountID: "b", Date: day.Add(2 * time.Hour)}},
	})
	x.accounts["a"] = &config.Account{ID: "a", Email: "me@example.com", ExtraAddresses: []string{"old@example.com"}}

	if got := x.HiddenCount("INBOX", []string{"a"}); got != 2 {
		t.Fatalf("HiddenCount = %d, want 2", got)
	}
	gotHidden := x.Hidden("INBOX", []string{"a", "b"})
	if len(gotHidden) != 3 || gotHidden[0].UID != 9 || gotHidden[1].UID != 2 || gotHidden[2].UID != 3 {
		t.Fatalf("Hidden order = %v", gotHidden)
	}
	gotCounts := x.HiddenRecipients("a", "INBOX")
	wantCounts := []AddressCount{{Address: "z@example.com", Count: 2}, {Address: "a@example.com", Count: 1}, {Address: "b@example.com", Count: 1}}
	if !slices.Equal(gotCounts, wantCounts) {
		t.Fatalf("HiddenRecipients = %+v, want %+v", gotCounts, wantCounts)
	}
	if got := subjects(x.Search(backend.ParseSearchQuery("verification"), "INBOX", []string{"a"}, 50)); !slices.Equal(got, []string{"visible verification"}) {
		t.Fatalf("local search exposed hidden mail: %q", got)
	}
}

func TestStartSyncRefusesARunningOrJustFinishedSync(t *testing.T) {
	x := New()
	if !x.StartSync("a", "INBOX", time.Minute) {
		t.Fatal("first sync refused")
	}
	if x.StartSync("a", "INBOX", 0) {
		t.Fatal("a second sync of the same folder started while the first runs")
	}
	x.FinishSync("a", "INBOX")
	if x.StartSync("a", "INBOX", time.Minute) {
		t.Fatal("sync started again right after one finished")
	}
	if !x.StartSync("a", "INBOX", 0) {
		t.Fatal("sync refused after the wait passed")
	}
}

func TestCachedHeadersKeepReplyRecipients(t *testing.T) {
	email := fetcher.Email{
		UID: 1, AccountID: "a", Subject: "delivery",
		To:                 []string{"unrelated@example.net", "main@example.com"},
		DeliveryRecipients: []string{"course@example.com"},
	}
	restored := emailFromCache(emailToCache(email))
	if !slices.Equal(restored.To, email.To) || !slices.Equal(restored.DeliveryRecipients, email.DeliveryRecipients) {
		t.Fatalf("cached header lost reply recipients: %+v", restored)
	}
	found := indexWith("a", "INBOX", restored).Search(backend.ParseSearchQuery("delivery"), "INBOX", []string{"a"}, 1)
	if len(found) != 1 || !slices.Equal(found[0].DeliveryRecipients, email.DeliveryRecipients) {
		t.Fatalf("local search lost delivery recipients: %+v", found)
	}
}
