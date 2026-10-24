package app

import (
	"slices"
	"testing"

	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
)

func TestAddressDiscoveryRespectsPreferencesChangedWhileScanning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{DisableDaemon: true, HasSeenSetupGuide: true, Accounts: []config.Account{
		{ID: "one", Email: "me@example.com", DiscoverExtraAddresses: true, ExtraAddresses: []string{"known@example.com"}},
		{ID: "two", Email: "other@example.com"},
	}}
	m := NewModel(cfg, nil, nil, false, nil, nil)
	// These choices can change after the background scan has captured its account.
	cfg.Accounts[0].IgnoredAddresses = []string{"ignored@example.com"}
	cfg.Accounts[1].ExtraAddresses = []string{"belongs-to-other@example.com"}
	cfg.RefreshOtherAccountAddresses()
	m.handleAddressDiscoveryResult(addressDiscoveryMsg{accountID: "one", addresses: []fetcher.DiscoveredAddress{
		{Address: "ignored@example.com", SentCount: 3, HiddenCount: 3},
		{Address: "belongs-to-other@example.com", SentCount: 3, HiddenCount: 3},
		{Address: "known@example.com", SentCount: 3, HiddenCount: 3},
		{Address: "found@example.com", SentCount: 3, HiddenCount: 3},
	}})
	saved, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Accounts[0].ExtraAddresses; !slices.Equal(got, []string{"known@example.com", "found@example.com"}) {
		t.Fatalf("discovery saved %v despite newer account choices", got)
	}
	cfg.Accounts[0].DiscoverExtraAddresses = false
	m.handleAddressDiscoveryResult(addressDiscoveryMsg{accountID: "one", addresses: []fetcher.DiscoveredAddress{
		{Address: "late@example.com", SentCount: 3, HiddenCount: 3},
	}})
	if slices.Contains(cfg.Accounts[0].ExtraAddresses, "late@example.com") {
		t.Fatal("discovery added an address after being disabled")
	}
}
