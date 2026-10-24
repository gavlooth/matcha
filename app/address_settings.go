package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
	"github.com/floatpane/matcha/internal/emailstore"
)

func accountAddressSettings(cfg *config.Config) map[string]string {
	settings := make(map[string]string)
	if cfg != nil {
		for i := range cfg.Accounts {
			account := &cfg.Accounts[i]
			settings[account.ID] = fetcher.AddressSettings(account)
		}
	}
	return settings
}

// refreshAddressSettingsCmd updates cached views when addresses or show rules
// change, even when no automatic rule moves or marks a message.
func (m *Model) refreshAddressSettingsCmd() tea.Cmd {
	if m.config == nil {
		return nil
	}
	settings := accountAddressSettings(m.config)
	var cmds []tea.Cmd
	changed := false
	folders := []string{emailstore.FolderInbox}
	if m.folderInbox != nil && m.folderInbox.GetCurrentFolder() != emailstore.FolderInbox {
		folders = append(folders, m.folderInbox.GetCurrentFolder())
	}
	for _, account := range m.config.Accounts {
		policyChanged := settings[account.ID] != m.addressSettings[account.ID]
		needsDiscovery := account.DiscoverExtraAddresses && !m.discoveredAddressAccounts[account.ID] &&
			(account.Protocol == "" || account.Protocol == "imap")
		if !policyChanged && !needsDiscovery {
			continue
		}
		changed = changed || policyChanged
		if m.headerIndex == nil {
			continue
		}
		m.headerIndex.SetAccount(&account)
		for _, folder := range folders {
			if !policyChanged && folder != emailstore.FolderInbox {
				continue
			}
			if m.headerIndex.StartSync(account.ID, folder, 0) {
				cmds = append(cmds, m.headerSyncStepCmd(account, folder))
			}
		}
	}
	m.addressSettings = settings
	if changed {
		if m.store != nil {
			clear(m.store.FolderEmails)
		}
		if m.folderInbox != nil {
			cmds = append(cmds, m.refreshFolderEmails(m.folderInbox.GetCurrentFolder()))
		}
	}
	return tea.Batch(cmds...)
}
