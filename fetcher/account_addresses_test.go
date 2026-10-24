package fetcher

import (
	"testing"

	"github.com/floatpane/matcha/config"
)

func TestAccountAddressesIsOwn(t *testing.T) {
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
