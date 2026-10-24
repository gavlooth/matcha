package app

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
	"github.com/floatpane/matcha/tui"
)

// headerSyncBatch is how many missing headers one sync step fetches; each
// step saves its progress, so a long first sync can stop and resume.
const headerSyncBatch = 500

// headerSyncWait is how long after a finished sync of a folder opening the
// search box again does not sync it again.
const headerSyncWait = 30 * time.Second

type headerIndexLoadedMsg struct {
	accountID string
	folder    string
}

type headerSyncStepMsg struct {
	accountID string
	folder    string
	remaining int
	err       error
}

type addressDiscoveryMsg struct {
	accountID string
	addresses []fetcher.DiscoveredAddress
	err       error
}

// updateHeaderIndexCmd loads the saved header copy of folder for the account
// (every account when accountID is empty) and then brings it up to date.
func (m *Model) updateHeaderIndexCmd(folder, accountID string) tea.Cmd {
	if m.config == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, account := range m.config.Accounts {
		if accountID != "" && account.ID != accountID {
			continue
		}
		id := account.ID
		cmds = append(cmds, func() tea.Msg {
			m.headerIndex.Load(id, folder)
			return headerIndexLoadedMsg{accountID: id, folder: folder}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) headerSyncStepCmd(account config.Account, folder string) tea.Cmd {
	return func() tea.Msg {
		remaining, err := m.headerIndex.SyncStep(&account, folder, headerSyncBatch)
		return headerSyncStepMsg{accountID: account.ID, folder: folder, remaining: remaining, err: err}
	}
}

func (m *Model) discoverOwnAddressesCmd(account config.Account, hiddenRecipients map[string]int) tea.Cmd {
	return func() tea.Msg {
		addresses, err := fetcher.DiscoverOwnAddresses(&account, hiddenRecipients)
		return addressDiscoveryMsg{accountID: account.ID, addresses: addresses, err: err}
	}
}

func localSearchIndexChanged() tea.Msg { return tui.LocalSearchIndexChangedMsg{} }

func (m *Model) accountByID(id string) (config.Account, bool) {
	if m.config != nil {
		for _, account := range m.config.Accounts {
			if account.ID == id {
				return account, true
			}
		}
	}
	return config.Account{}, false
}

// handleHeaderIndexLoaded shows the loaded copy in an open search box and
// starts the first sync step unless the folder was synced a moment ago.
func (m *Model) handleHeaderIndexLoaded(msg headerIndexLoadedMsg) tea.Cmd {
	account, ok := m.accountByID(msg.accountID)
	if !ok || !m.headerIndex.StartSync(msg.accountID, msg.folder, headerSyncWait) {
		return localSearchIndexChanged
	}
	return tea.Batch(localSearchIndexChanged, m.headerSyncStepCmd(account, msg.folder))
}

// handleHeaderSyncStep refreshes an open search box and runs the next step
// while headers are still missing.
func (m *Model) handleHeaderSyncStep(msg headerSyncStepMsg) tea.Cmd {
	account, ok := m.accountByID(msg.accountID)
	if !ok {
		// The account was removed while this step ran; the step may have
		// saved its headers after the account's files were deleted.
		if err := config.RemoveAccountHeaderIndex(msg.accountID); err != nil {
			log.Printf("header index: could not remove files of removed account %s: %v", msg.accountID, err)
		}
	}
	if msg.err != nil || msg.remaining == 0 || !ok {
		if msg.err != nil && !errors.Is(msg.err, fetcher.ErrHeaderSyncNotSupported) {
			log.Printf("header index: could not sync %s for %s: %v", msg.folder, msg.accountID, msg.err)
		}
		m.headerIndex.FinishSync(msg.accountID, msg.folder)
		if msg.err == nil && msg.remaining == 0 && ok && msg.folder == "INBOX" &&
			account.DiscoverExtraAddresses && !m.discoveredAddressAccounts[account.ID] {
			if m.discoveredAddressAccounts == nil {
				m.discoveredAddressAccounts = make(map[string]bool)
			}
			m.discoveredAddressAccounts[account.ID] = true
			recipients := m.headerIndex.HiddenRecipients(account.ID, "INBOX")
			hiddenRecipients := make(map[string]int, len(recipients))
			for _, recipient := range recipients {
				hiddenRecipients[recipient.Address] = recipient.Count
			}
			return tea.Batch(
				localSearchIndexChanged,
				m.discoverOwnAddressesCmd(account, hiddenRecipients),
			)
		}
		return localSearchIndexChanged
	}
	return tea.Batch(localSearchIndexChanged, m.headerSyncStepCmd(account, msg.folder))
}

func (m *Model) handleAddressDiscoveryResult(msg addressDiscoveryMsg) tea.Cmd {
	if m.config == nil {
		return nil
	}
	account := m.config.GetAccountByID(msg.accountID)
	if account == nil || !account.DiscoverExtraAddresses {
		return nil
	}
	if msg.err != nil {
		log.Printf("address discovery for %s failed: %v", msg.accountID, msg.err)
		return m.showErrorCmd(fmt.Sprintf("Could not discover addresses for %s; check connectivity and the Sent folder.", account.Email))
	}
	if len(msg.addresses) == 0 {
		return nil
	}

	previousAddresses := account.ExtraAddresses
	account.ExtraAddresses = slices.Clone(account.ExtraAddresses)
	notices := make([]string, 0, len(msg.addresses))
	name := account.Name
	if name == "" {
		name = account.Email
	}
	for _, address := range msg.addresses {
		if fetcher.IsOwnAddress(account, address.Address) ||
			fetcher.IsOwnAddress(&config.Account{ServiceProvider: account.ServiceProvider, ExtraAddresses: account.IgnoredAddresses}, address.Address) ||
			fetcher.IsOwnAddress(&config.Account{ServiceProvider: account.ServiceProvider, ExtraAddresses: account.OtherAccountAddresses}, address.Address) {
			continue
		}
		account.ExtraAddresses = append(account.ExtraAddresses, address.Address)
		notices = append(notices, fmt.Sprintf(
			"Added %s to %s: sent from it %d times, %d hidden mails to it",
			address.Address, name, address.SentCount, address.HiddenCount,
		))
	}
	if len(notices) == 0 {
		return nil
	}
	if err := config.SaveConfig(m.config); err != nil {
		account.ExtraAddresses = previousAddresses
		m.config.RefreshOtherAccountAddresses()
		log.Printf("address discovery for %s could not save config: %v", msg.accountID, err)
		return m.showErrorCmd(fmt.Sprintf("Could not save discovered addresses for %s; check configuration file access.", account.Email))
	}
	m.afterConfigSaved()
	return tea.Batch(m.showInfoCmd(strings.Join(notices, "; ")), m.refreshAddressSettingsCmd())
}
