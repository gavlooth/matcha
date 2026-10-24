package fetcher

import (
	"slices"
	"strconv"
	"testing"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/floatpane/matcha/config"
)

func TestDiscoverOwnAddressesRequiresSentAndHiddenEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := memIMAP(t)
	if err := client.Create("Sent", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	appendSent := func(from string, count int) {
		t.Helper()
		for i := range count {
			appendMessageFrom(t, client, "Sent", from, "recipient@example.com", from+strconv.Itoa(i))
		}
	}
	appendSent("exact@example.com", 3)
	appendSent("ordered@example.com", 4)
	appendSent("few-sent@example.com", 2)
	appendSent("notification@example.com", 10)
	appendSent("me@example.com", 3)
	appendSent("ignored@example.com", 3)
	appendSent("other@example.com", 3)
	appendSent("user+tag@gmail.com", 3)

	account := &config.Account{
		ID:                    "discover-address-test",
		Email:                 "me@example.com",
		FetchEmail:            "me@example.com",
		ServiceProvider:       config.ProviderCustom,
		ExtraAddresses:        []string{"known@example.com"},
		IgnoredAddresses:      []string{"ignored@example.com"},
		OtherAccountAddresses: []string{"other@example.com"},
	}

	previousDial := imapPool.dial
	imapPool.dial = func(*config.Account) (*imapclient.Client, error) { return client, nil }
	t.Cleanup(func() {
		imapPool.dial = previousDial
		CloseIMAPPool()
	})

	got, err := DiscoverOwnAddresses(account, map[string]int{
		"exact@example.com":        3,
		"ordered@example.com":      4,
		"few-sent@example.com":     3,
		"notification@example.com": 0,
		"me@example.com":           3,
		"ignored@example.com":      3,
		"other@example.com":        3,
		"user@gmail.com":           3,
		"known@example.com":        3,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []DiscoveredAddress{
		{Address: "ordered@example.com", SentCount: 4, HiddenCount: 4},
		{Address: "exact@example.com", SentCount: 3, HiddenCount: 3},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("DiscoverOwnAddresses() = %#v, want %#v", got, want)
	}
}

func TestDiscoverOwnAddressesRequiresThreeHiddenMessages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := memIMAP(t)
	if err := client.Create("Sent", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		appendMessageFrom(t, client, "Sent", "candidate@example.com", "recipient@example.com", "message"+strconv.Itoa(i))
	}
	account := &config.Account{ID: "discover-hidden-threshold-test", Email: "me@example.com", ServiceProvider: config.ProviderCustom}
	previousDial := imapPool.dial
	imapPool.dial = func(*config.Account) (*imapclient.Client, error) { return client, nil }
	t.Cleanup(func() {
		imapPool.dial = previousDial
		CloseIMAPPool()
	})

	for _, hiddenCount := range []int{2, 3} {
		got, err := DiscoverOwnAddresses(account, map[string]int{"candidate@example.com": hiddenCount})
		if err != nil {
			t.Fatal(err)
		}
		if (len(got) == 1) != (hiddenCount == 3) {
			t.Errorf("hidden count %d: got %#v", hiddenCount, got)
		}
	}
}

func TestDiscoverOwnAddressesUsesGmailAddressNormalization(t *testing.T) {
	client := memIMAP(t)
	sentMailbox := "[Gmail]/Sent Mail"
	if err := client.Create(sentMailbox, nil).Wait(); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		appendMessageFrom(t, client, sentMailbox, "u.ser+tag@gmail.com", "recipient@example.com", "message"+strconv.Itoa(i))
	}
	account := &config.Account{
		ID:              "discover-gmail-own-test",
		Email:           "user@gmail.com",
		FetchEmail:      "user@gmail.com",
		ServiceProvider: config.ProviderGmail,
	}
	previousDial := imapPool.dial
	imapPool.dial = func(*config.Account) (*imapclient.Client, error) { return client, nil }
	t.Cleanup(func() {
		imapPool.dial = previousDial
		CloseIMAPPool()
	})

	got, err := DiscoverOwnAddresses(account, map[string]int{"u.ser+tag@gmail.com": 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("DiscoverOwnAddresses() = %#v, want no account-owned address", got)
	}
}

func TestDiscoverOwnAddressesSkipsNonIMAPAccounts(t *testing.T) {
	got, err := DiscoverOwnAddresses(&config.Account{Protocol: "pop3"}, nil)
	if err != nil || got != nil {
		t.Fatalf("DiscoverOwnAddresses() = %#v, %v; want nil, nil", got, err)
	}
}
