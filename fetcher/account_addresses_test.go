package fetcher

import (
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/floatpane/matcha/config"
)

func TestAccountAddressesIsOwn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	custom := newAccountAddresses(&config.Account{Email: "login@example.com", FetchEmail: "me@example.com", ExtraAddresses: []string{"Old <old@example.com>", "  "}})
	gmail := newAccountAddresses(&config.Account{ServiceProvider: "gmail", Email: "me@gmail.com", ExtraAddresses: []string{"other.me@gmail.com"}})

	cases := []struct {
		name      string
		addresses accountAddresses
		candidate string
		want      bool
	}{
		{"fetch address", custom, "ME@example.com", true},
		{"extra address with display name", custom, "Someone <old@example.com>", true},
		{"login address is not a delivery address when a fetch address is set", custom, "login@example.com", false},
		{"blank extra address matches nothing", custom, "", false},
		{"unrelated address", custom, "old@example.org", false},
		{"gmail dots and tags apply to extra addresses", gmail, "otherme+news@gmail.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.addresses.isOwn(tc.candidate); got != tc.want {
				t.Errorf("isOwn(%q) = %v, want %v", tc.candidate, got, tc.want)
			}
		})
	}
}

func TestAccountAddressesNamedInDeliveryHeaders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	addresses := newAccountAddresses(&config.Account{Email: "me@example.com", ExtraAddresses: []string{"old@example.com"}})
	cases := map[string]bool{
		"Delivered-To: <old@example.com>\r\n\r\n":                      true,
		"X-Original-To: list@example.com, Me <me@example.com>\r\n\r\n": true,
		"X-Forwarded-To: someone@example.com\r\n\r\n":                  false,
		"References: <old@example.com>\r\nTo: me@example.com\r\n\r\n":  false,
		"": false,
	}
	for header, want := range cases {
		if got := addresses.namedInDeliveryHeaders([]byte(header)); got != want {
			t.Errorf("namedInDeliveryHeaders(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestAccountAddressesAlwaysShowsMailFrom(t *testing.T) {
	addresses := newAccountAddresses(&config.Account{Email: "me@example.com", AlwaysShowMailFrom: []string{"@Uni.edu", "boss@corp.com"}})
	cases := map[string]bool{
		"Prof <prof@uni.edu>":   true,
		"prof@ee.uni.edu":       true,
		"prof@evil-uni.edu":     false,
		"prof@uni.edu.evil.com": false,
		"boss@corp.com":         true,
		"intern@corp.com":       false,
		"no-address":            false,
	}
	for sender, want := range cases {
		if got := addresses.alwaysShowsMailFrom(sender); got != want {
			t.Errorf("alwaysShowsMailFrom(%q) = %v, want %v", sender, got, want)
		}
	}
}

func TestAddressSettingsIgnoresOrderAndCase(t *testing.T) {
	a := &config.Account{Email: "me@example.com", ExtraAddresses: []string{"b@example.com", "A@example.com"}, AlwaysShowMailFrom: []string{"uni.edu"}}
	b := &config.Account{Email: "ME@example.com", ExtraAddresses: []string{"a@example.com", "b@example.com"}, AlwaysShowMailFrom: []string{"@uni.edu"}}
	if AddressSettings(a) != AddressSettings(b) {
		t.Errorf("the same settings written differently must not refetch the folder: %q vs %q", AddressSettings(a), AddressSettings(b))
	}
	b.AlwaysShowMailFrom = nil
	if AddressSettings(a) == AddressSettings(b) {
		t.Error("removing a sender must change the address settings")
	}
}
func TestMessageMatchesUnaddressedMailOnlyWhenNoAccountIsNamed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	message := &imapclient.FetchMessageBuffer{Envelope: &imap.Envelope{
		From:    []imap.Address{{Mailbox: "teacher", Host: "school.example"}},
		Subject: "Course update",
	}}
	section := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader}
	account := &config.Account{
		ID: "course", Email: "course@example.com", FetchEmail: "course@example.com",
		ShowUnaddressedMail: true, OtherAccountAddresses: []string{"personal@example.com"},
	}
	addresses := newAccountAddresses(account)
	if !messageMatchesAccount(message, nil, addresses, false, section) {
		t.Fatal("opted-in account hid unaddressed mail")
	}
	if messageMatchesAccount(message, nil, addresses, true, section) {
		t.Fatal("Sent mail was shown without an account sender address")
	}
	if messageMatchesAccount(message, []string{"personal@example.com"}, addresses, false, section) {
		t.Fatal("mail addressed to another configured account was shown")
	}
	account.ShowUnaddressedMail = false
	if messageMatchesAccount(message, nil, newAccountAddresses(account), false, section) {
		t.Fatal("flag off changed the existing hidden-mail behavior")
	}
}

func TestShowInAccountRuleMakesMatchingBccMailVisible(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	message := &imapclient.FetchMessageBuffer{Envelope: &imap.Envelope{
		From:    []imap.Address{{Mailbox: "teacher", Host: "school.example"}},
		Subject: "Course update",
	}}
	section := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader}
	account := &config.Account{ID: "course", Email: "course@example.com"}
	rule := config.MailRule{ID: "show-course", Name: "Course", Enabled: true, AccountID: account.ID, From: "teacher", Subject: "update", ShowInAccount: true}
	if err := config.SaveMailRules([]config.MailRule{rule}); err != nil {
		t.Fatal(err)
	}
	if !messageMatchesAccount(message, nil, newAccountAddresses(account), false, section) {
		t.Fatal("matching show rule hid Bcc mail")
	}
	rule.Enabled = false
	if err := config.SaveMailRules([]config.MailRule{rule}); err != nil {
		t.Fatal(err)
	}
	if messageMatchesAccount(message, nil, newAccountAddresses(account), false, section) {
		t.Fatal("disabled show rule made Bcc mail visible")
	}
}
