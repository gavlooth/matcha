package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/floatpane/matcha/config"
	"github.com/google/uuid"
)

const (
	ruleName = iota
	ruleAccount
	ruleFrom
	ruleTo
	ruleSubject
	ruleMoveTo
	ruleMarkRead
	ruleEnabled
	ruleSave
	ruleCancel
	ruleFieldCount
)

type ruleSettings struct {
	loaded     bool
	loadError  string
	errorText  string
	status     string
	list       []config.MailRule
	cursor     int
	confirming bool
	editing    bool
	editIndex  int
	draft      config.MailRule
	focus      int
	inputs     [5]textinput.Model
	help       bool
	helpScroll int
}

func (m *Settings) settingsMenuWidth() int {
	if m.width <= 0 {
		return 30
	}
	return min(30, max(10, m.width/3))
}

func (m *Settings) settingsMenuWindow() (int, int) {
	count := int(settingsCategoryCount)
	if m.height > 0 {
		count = min(count, max(1, m.height-6))
	}
	first := max(0, min(m.menuCursor-count/2, int(settingsCategoryCount)-count))
	return first, count
}

func (m *Settings) ruleWidth() int {
	if m.width <= 0 {
		return 60
	}
	return max(1, m.width-6-m.settingsMenuWidth())
}

func (m *Settings) loadRules() {
	m.rules.loaded = true
	m.rules.errorText = ""
	m.rules.status = ""
	list, err := config.LoadMailRules()
	if err != nil {
		m.rules.loadError = "Cannot load rules; changes blocked: " + err.Error()
		return
	}
	m.rules.loadError = ""
	m.rules.list = list
	m.rules.cursor = min(m.rules.cursor, len(list))
}

// Commit only after a successful load, preserving the visible list on failure.
// Re-reading also prevents an open settings view from replacing external edits.
func (m *Settings) saveRules(next []config.MailRule) bool {
	m.rules.errorText = ""
	m.rules.status = ""
	if m.rules.loadError != "" {
		return false
	}
	current, err := config.LoadMailRules()
	if err != nil {
		m.rules.loadError = "Cannot reload rules; changes blocked: " + err.Error()
		return false
	}
	if !slices.Equal(current, m.rules.list) {
		m.rules.errorText = "Rules changed outside this view. Cancel, then press r to reload."
		return false
	}
	if err := config.SaveMailRules(next); err != nil {
		m.rules.errorText = "Cannot save rules: " + err.Error()
		return false
	}
	saved, err := config.LoadMailRules()
	if err != nil {
		m.rules.loadError = "Rules saved, but cannot reload; changes blocked: " + err.Error()
		return false
	}
	m.rules.list = saved
	m.rules.status = "Rules saved."
	return true
}

func (m *Settings) beginRuleEdit(index int) tea.Cmd {
	if m.rules.loadError != "" {
		return nil
	}
	m.rules.errorText = ""
	m.rules.status = ""
	m.rules.editIndex = index
	m.rules.draft = config.MailRule{}
	if index < len(m.rules.list) {
		m.rules.draft = m.rules.list[index]
	} else {
		m.rules.draft.ID = uuid.NewString()
	}
	values := []string{m.rules.draft.Name, m.rules.draft.From, m.rules.draft.To, m.rules.draft.Subject, m.rules.draft.MoveTo}
	for i, value := range values {
		input := textinput.New()
		input.Prompt = "> "
		input.SetStyles(ThemedTextInputStyles())
		input.SetValue(value)
		m.rules.inputs[i] = input
	}
	m.rules.editing = true
	m.rules.focus = ruleName
	m.resizeRuleInputs()
	return m.focusRuleField(ruleName)
}

func ruleInputIndex(field int) int {
	switch field {
	case ruleName:
		return 0
	case ruleFrom, ruleTo, ruleSubject, ruleMoveTo:
		return field - 1
	default:
		return -1
	}
}

func (m *Settings) resizeRuleInputs() {
	if !m.rules.editing {
		return
	}
	for i := range m.rules.inputs {
		m.rules.inputs[i].SetWidth(max(1, m.ruleWidth()-4))
	}
}

func (m *Settings) focusRuleField(field int) tea.Cmd {
	m.rules.focus = (field + ruleFieldCount) % ruleFieldCount
	for i := range m.rules.inputs {
		m.rules.inputs[i].Blur()
	}
	if i := ruleInputIndex(m.rules.focus); i >= 0 {
		return m.rules.inputs[i].Focus()
	}
	return nil
}

func (m *Settings) updateRuleInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if i := ruleInputIndex(m.rules.focus); i >= 0 {
		var cmd tea.Cmd
		m.rules.inputs[i], cmd = m.rules.inputs[i].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Settings) closeRuleForm() {
	for i := range m.rules.inputs {
		m.rules.inputs[i].Blur()
	}
	m.rules.editing = false
	m.rules.errorText = ""
}

func (m *Settings) commitRuleForm() {
	rule := m.rules.draft
	rule.Name = strings.TrimSpace(m.rules.inputs[0].Value())
	rule.From = m.rules.inputs[1].Value()
	rule.To = m.rules.inputs[2].Value()
	rule.Subject = m.rules.inputs[3].Value()
	rule.MoveTo = m.rules.inputs[4].Value()
	if err := config.ValidateMailRule(rule); err != nil {
		m.rules.errorText = "Cannot save rule: " + err.Error()
		return
	}
	next := slices.Clone(m.rules.list)
	if m.rules.editIndex < len(next) {
		next[m.rules.editIndex] = rule
	} else {
		next = append(next, rule)
	}
	if m.saveRules(next) {
		m.rules.cursor = m.rules.editIndex
		m.closeRuleForm()
	}
}

func (m *Settings) cycleRuleAccount(direction int) {
	count := len(m.cfg.Accounts) + 1 // All accounts, then configured accounts.
	index := 0
	for i, account := range m.cfg.Accounts {
		if account.ID == m.rules.draft.AccountID {
			index = i + 1
			break
		}
	}
	// Keep an unavailable scope until the user explicitly changes it.
	if index == 0 && m.rules.draft.AccountID != "" {
		index = count
		count++
	}
	next := (index + direction + count) % count
	if next == 0 {
		m.rules.draft.AccountID = ""
	} else if next <= len(m.cfg.Accounts) {
		m.rules.draft.AccountID = m.cfg.Accounts[next-1].ID
	}
}

func (m *Settings) ruleAccountLabel(id string) string {
	if id == "" {
		return "All configured accounts"
	}
	for _, account := range m.cfg.Accounts {
		if account.ID == id {
			label := account.Email
			if label == "" {
				label = account.FetchEmail
			}
			if label == "" {
				label = account.ID
			}
			if account.Name != "" {
				label += " (" + account.Name + ")"
			}
			return ruleSingleLine(label)
		}
	}
	return "Unavailable account (scope preserved)"
}

func (m *Settings) activateRuleField() tea.Cmd {
	switch m.rules.focus {
	case ruleAccount:
		m.cycleRuleAccount(1)
	case ruleMarkRead:
		m.rules.draft.MarkRead = !m.rules.draft.MarkRead
	case ruleEnabled:
		m.rules.draft.Enabled = !m.rules.draft.Enabled
	case ruleSave:
		m.commitRuleForm()
	case ruleCancel:
		m.closeRuleForm()
	default:
		return m.focusRuleField(m.rules.focus + 1)
	}
	return nil
}

func (m *Settings) updateRules(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !m.rules.loaded {
		m.loadRules()
	}
	key := msg.String()
	if m.rules.help {
		switch key {
		case "esc", "?", "q":
			m.rules.help = false
		case "up", "k":
			m.rules.helpScroll = max(0, m.rules.helpScroll-1)
		case keyDown, "j":
			m.rules.helpScroll = min(m.ruleHelpMaxScroll(), m.rules.helpScroll+1)
		}
		return m, nil
	}
	if m.rules.confirming {
		switch key {
		case "y", "Y":
			next := slices.Delete(slices.Clone(m.rules.list), m.rules.cursor, m.rules.cursor+1)
			if m.saveRules(next) {
				m.rules.cursor = min(m.rules.cursor, len(next))
			}
			m.rules.confirming = false
		case "n", "N", "esc":
			m.rules.confirming = false
		}
		return m, nil
	}
	if m.rules.editing {
		switch key {
		case "esc":
			m.closeRuleForm()
			return m, nil
		case "ctrl+s":
			m.commitRuleForm()
			return m, nil
		case "tab", keyDown:
			return m, m.focusRuleField(m.rules.focus + 1)
		case "shift+tab", "up":
			return m, m.focusRuleField(m.rules.focus - 1)
		case keyEnter:
			return m, m.activateRuleField()
		}
		// Printable keys (including space, j/k, e/d) and editing keys belong
		// entirely to the text input. Left never changes settings panes here.
		if ruleInputIndex(m.rules.focus) >= 0 {
			return m.updateRuleInput(msg)
		}
		switch key {
		case "space", " ":
			return m, m.activateRuleField()
		case keyLeft:
			if m.rules.focus == ruleAccount {
				m.cycleRuleAccount(-1)
			}
		case keyRight:
			if m.rules.focus == ruleAccount {
				m.cycleRuleAccount(1)
			}
		}
		return m, nil
	}
	switch key {
	case "esc", keyLeft:
		m.activePane = PaneMenu
	case "?":
		m.rules.help = true
		m.rules.helpScroll = 0
	case "r":
		m.loadRules()
	case "up", "k":
		m.rules.cursor = (m.rules.cursor + len(m.rules.list)) % (len(m.rules.list) + 1)
	case keyDown, "j":
		m.rules.cursor = (m.rules.cursor + 1) % (len(m.rules.list) + 1)
	case "n":
		return m, m.beginRuleEdit(len(m.rules.list))
	case keyEnter, "e":
		return m, m.beginRuleEdit(m.rules.cursor)
	case "space", " ":
		if m.rules.cursor < len(m.rules.list) && m.rules.loadError == "" {
			next := slices.Clone(m.rules.list)
			next[m.rules.cursor].Enabled = !next[m.rules.cursor].Enabled
			m.saveRules(next)
		}
	case "d":
		if m.rules.cursor < len(m.rules.list) && m.rules.loadError == "" {
			m.rules.confirming = true
		}
	case "K", "J":
		if m.rules.cursor >= len(m.rules.list) || m.rules.loadError != "" {
			break
		}
		target := m.rules.cursor + 1
		if key == "K" {
			target = m.rules.cursor - 1
		}
		if target >= 0 && target < len(m.rules.list) {
			next := slices.Clone(m.rules.list)
			next[m.rules.cursor], next[target] = next[target], next[m.rules.cursor]
			if m.saveRules(next) {
				m.rules.cursor = target
			}
		}
	}
	return m, nil
}

func ruleSingleLine(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}

func ruleChecked(value bool) string {
	if value {
		return "[x]"
	}
	return "[ ]"
}

func (m *Settings) ruleRows() ([]string, []int) {
	var rows []string
	var positions []int
	if !m.rules.editing {
		for i, rule := range m.rules.list {
			positions = append(positions, len(rows))
			label := fmt.Sprintf("%d. %s %s", i+1, ruleChecked(rule.Enabled), ruleSingleLine(rule.Name))
			rows = append(rows, m.contentItemStyle(m.rules.cursor == i).Render(m.contentCursor(m.rules.cursor == i)+ansi.Truncate(label, max(1, m.ruleWidth()-4), "…")))
		}
		positions = append(positions, len(rows))
		rows = append(rows, m.contentItemStyle(m.rules.cursor == len(m.rules.list)).Render(m.contentCursor(m.rules.cursor == len(m.rules.list))+"Add rule (disabled by default)"))
		return rows, positions
	}
	labels := []string{"Name", "Account scope", "From contains", "To contains", "Subject contains", "Move to folder (blank = no move)", "Mark read", "Enabled", "Save", "Cancel"}
	for field, label := range labels {
		positions = append(positions, len(rows))
		selected := m.rules.focus == field
		if input := ruleInputIndex(field); input >= 0 {
			rows = append(rows, m.contentFocusStyle().Render(m.contentCursor(selected)+label))
			rows = append(rows, "  "+m.rules.inputs[input].View())
			continue
		}
		switch field {
		case ruleAccount:
			rows = append(rows, m.contentItemStyle(selected).Render(m.contentCursor(selected)+label+" (←/→)"))
			rows = append(rows, "  "+m.ruleAccountLabel(m.rules.draft.AccountID))
			continue
		case ruleMarkRead:
			label = ruleChecked(m.rules.draft.MarkRead) + " " + label
		case ruleEnabled:
			label = ruleChecked(m.rules.draft.Enabled) + " " + label
		}
		rows = append(rows, m.contentItemStyle(selected).Render(m.contentCursor(selected)+label))
	}
	return rows, positions
}

func (m *Settings) ruleFooter() []string {
	text := "↑/↓ select · Enter edit/add · n new · Space enable/disable · d delete · K/J reorder · r reload · ? help · Esc menu"
	if m.rules.editing {
		text = "Tab/↑/↓ fields · ←/→ account · Space/Enter toggle · Ctrl+S save · Esc cancel"
	}
	if m.rules.confirming {
		text = "Delete selected rule? y confirms · n/Esc cancels"
	}
	var footer []string
	for _, message := range []string{m.rules.loadError, m.rules.errorText} {
		if message != "" {
			footer = append(footer, dangerStyle.Render(ansi.Hardwrap(ruleSingleLine(message), m.ruleWidth(), true)))
		}
	}
	if m.rules.status != "" {
		footer = append(footer, successStyle.Render(m.rules.status))
	}
	footer = append(footer, helpStyle.Render(ansi.Hardwrap(text, m.ruleWidth(), true)))
	return strings.Split(strings.Join(footer, "\n"), "\n")
}

// The body window follows selection, so every field/action remains reachable
// even when a terminal is too short to display the entire form or rule list.
func (m *Settings) ruleLayout() (header, body, footer []string, start int) {
	header = []string{
		titleStyle.Render("Rules"),
		helpStyle.Render("Local only · AND · first match wins"),
		helpStyle.Render("Future mail only · existing mail left alone · ? help"),
	}
	rows, positions := m.ruleRows()
	footer = m.ruleFooter()
	height := 30
	if m.height > 0 {
		height = max(3, m.height-docStyle.GetVerticalFrameSize())
	}
	// Keep errors ahead of shortcut help if vertical room is scarce.
	footer = footer[:min(len(footer), max(1, height-len(header)-2))]
	available := max(1, height-len(header)-len(footer))
	focus := m.rules.cursor
	if m.rules.editing {
		focus = m.rules.focus
	}
	selected := positions[focus]
	if m.rules.editing && (ruleInputIndex(focus) >= 0 || focus == ruleAccount) {
		selected++ // Keep the input or account value visible, not just its label.
	}
	start = max(0, min(selected-available/2, len(rows)-available))
	body = rows[start:min(len(rows), start+available)]
	return header, body, footer, start
}

func (m *Settings) ruleHelpLines() []string {
	text := "Rules run locally while Matcha is open, for IMAP accounts only. They are not provider-side/Sieve rules.\n\nFrom, To and Subject are case-insensitive substring matches. Every nonempty condition must match (AND). Account scope can be all configured accounts or one account.\n\nThe first enabled matching rule wins, even if it only marks mail read. Use uppercase K/J in the list to move rules earlier/later.\n\nRules apply to future incoming mail only. New, edited or newly enabled rules establish their starting point at the next background check (up to 30 seconds after saving); existing inbox messages are left alone. Once that starting point is established, unchanged rules also process mail that arrives while Matcha is closed when it next runs. New rules start disabled.\n\nDestination is the exact IMAP folder name; leave it blank for no move. Moving to INBOX is not allowed. At least one condition and one action are required. Rules never delete or forward mail.\n\nList: Enter edits (or adds at Add rule), n creates, Space enables/disables, d requests deletion, r reloads. In forms use Tab/Shift+Tab or arrows between fields; text fields accept spaces and editing keys. Ctrl+S saves; Esc cancels without saving.\n\nChanges take effect automatically after saving. No mail is processed by this settings screen."
	return strings.Split(ansi.Hardwrap(text, m.ruleWidth(), true), "\n")
}

func (m *Settings) ruleHelpMaxScroll() int {
	height := 25
	if m.height > 0 {
		height = max(1, m.height-docStyle.GetVerticalFrameSize()-2)
	}
	return max(0, len(m.ruleHelpLines())-height)
}

func (m *Settings) viewRules() string {
	var lines []string
	if m.rules.help {
		rows := m.ruleHelpLines()
		start := min(m.rules.helpScroll, m.ruleHelpMaxScroll())
		height := len(rows) - m.ruleHelpMaxScroll()
		lines = append(lines, titleStyle.Render("Local rule policy"))
		lines = append(lines, rows[start:min(len(rows), start+height)]...)
		lines = append(lines, helpStyle.Render("↑/↓ scroll · Esc close"))
	} else {
		header, body, footer, _ := m.ruleLayout()
		lines = append(lines, header...)
		lines = append(lines, body...)
		lines = append(lines, footer...)
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.ruleWidth(), "…")
	}
	return strings.Join(lines, "\n")
}

func (m *Settings) updateRulesMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	key := tea.KeyDown
	if msg.Button == tea.MouseWheelUp {
		key = tea.KeyUp
	} else if msg.Button != tea.MouseWheelDown {
		return m, nil
	}
	return m.updateRules(tea.KeyPressMsg{Code: key})
}

func (m *Settings) updateRulesMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	m.activePane = PaneContent
	if m.rules.help || m.rules.confirming {
		return m, nil
	}
	header, body, _, start := m.ruleLayout()
	row := msg.Y - docStyle.GetMarginTop() - len(header)
	if row < 0 || row >= len(body) {
		return m, nil
	}
	_, positions := m.ruleRows()
	item := 0
	for i, position := range positions {
		if position > row+start {
			break
		}
		item = i
	}
	if m.rules.editing {
		cmd := m.focusRuleField(item)
		if ruleInputIndex(item) < 0 {
			return m, m.activateRuleField()
		}
		return m, cmd
	}
	if m.rules.cursor == item {
		return m, m.beginRuleEdit(item)
	}
	m.rules.cursor = item
	return m, nil
}
