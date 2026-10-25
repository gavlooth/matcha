package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/internal/vimedit"
)

// SetVimMode changes body editing without changing the message or other fields.
func (m *Composer) SetVimMode(enabled bool) {
	if enabled == (m.vimEditor != nil) {
		return
	}
	if enabled {
		m.vimEditor = vimedit.New()
	} else {
		m.vimEditor.Leave(&m.bodyInput)
		m.vimEditor = nil
	}
	m.spellShow = false
	m.spellSuggestions = nil
	m.emojiShow = false
	m.emojiSuggestions = nil
}

// IsVimEditing reports whether body keys belong to Vim rather than app shortcuts.
func (m *Composer) IsVimEditing() bool {
	return m.vimEditor != nil && m.focusIndex == focusBody &&
		!m.confirmingExit && !m.showAccountPicker && !m.showPluginPrompt &&
		len(m.wkdConfirmRecipients) == 0
}

func (m *Composer) updateVimBody(msg tea.Msg) tea.Cmd {
	cmd := m.vimEditor.Update(&m.bodyInput, msg)
	if m.vimEditor.Mode() != vimedit.Insert {
		m.spellShow = false
		m.spellSuggestions = nil
		m.emojiShow = false
		m.emojiSuggestions = nil
	}
	return cmd
}
