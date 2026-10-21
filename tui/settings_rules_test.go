package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/floatpane/matcha/config"
)

func newRuleSettingsTest(t *testing.T, rules []config.MailRule) *Settings {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if rules != nil {
		if err := config.SaveMailRules(rules); err != nil {
			t.Fatal(err)
		}
	}
	m := NewSettings(&config.Config{Accounts: []config.Account{
		{ID: "first-account", Name: "First account", Email: "first@example.com"},
		{ID: "second-account", Name: "Second account", Email: "second@example.com"},
	}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.activeCategory = CategoryRules
	m.menuCursor = int(CategoryRules)
	m.activePane = PaneContent
	m.loadRules()
	return m
}

func testMailRule(id string) config.MailRule {
	return config.MailRule{ID: id, Name: "Rule " + id, From: "example.com", MarkRead: true}
}

func ruleTypeText(m *Settings, text string) {
	for _, r := range text {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func ruleTestFile(t *testing.T) string {
	t.Helper()
	dir, err := config.GetConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "mail_rules.json")
}

func TestSettingsRulesCategoryKeyboardMouseAndWheel(t *testing.T) {
	m := newRuleSettingsTest(t, nil)
	m.activeCategory = CategoryGeneral
	m.menuCursor = 0
	m.activePane = PaneMenu
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.activeCategory != CategoryRules {
		t.Fatalf("up from first category selected %v, want Rules", m.activeCategory)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.activePane != PaneContent {
		t.Fatal("enter did not open Rules")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.menuCursor = int(CategoryContacts)
	m.activeCategory = CategoryContacts
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.activeCategory != CategoryRules {
		t.Fatal("mouse wheel did not reach Rules")
	}
	m.activeCategory = CategoryGeneral
	m.menuCursor = 0
	row := -1
	for y, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(line, "Rules") {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatal("Rules category is missing")
	}
	m.Update(tea.MouseClickMsg{X: 6, Y: row, Button: tea.MouseLeft})
	if m.activeCategory != CategoryRules || m.activePane != PaneContent {
		t.Fatalf("click on rendered Rules row did not open it: category %v, pane %v", m.activeCategory, m.activePane)
	}
}

func TestSettingsRuleFormTypingScopeSaveAndCancel(t *testing.T) {
	m := newRuleSettingsTest(t, nil)
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !m.rules.editing || m.rules.draft.Enabled {
		t.Fatal("new rule must open disabled")
	}
	ruleTypeText(m, "Jack and Jill")
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.rules.inputs[0].Value(); got != "Jack and Jil" {
		t.Fatalf("spaces, navigation letters or editing were intercepted: %q", got)
	}
	if m.activePane != PaneContent || m.rules.focus != ruleName {
		t.Fatal("editing a text field moved settings focus")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.rules.draft.AccountID != "first-account" {
		t.Fatalf("account chooser selected %q", m.rules.draft.AccountID)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.rules.draft.AccountID != "first-account" {
		t.Fatal("account chooser did not cycle both directions")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	ruleTypeText(m, "mail from example.com")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	ruleTypeText(m, "team@example.com")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	ruleTypeText(m, "project notes")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	ruleTypeText(m, "Project Notes")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if !m.rules.draft.MarkRead {
		t.Fatal("Space did not toggle Mark read")
	}
	m.focusRuleField(ruleSave)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.rules.editing || m.rules.errorText != "" {
		t.Fatalf("save failed: %s", m.rules.errorText)
	}
	stored, err := config.LoadMailRules()
	if err != nil || len(stored) != 1 {
		t.Fatalf("saved definitions: count %d, error %v", len(stored), err)
	}
	rule := stored[0]
	if rule.Name != "Jack and Jil" || rule.AccountID != "first-account" || rule.From != "mail from example.com" || rule.To != "team@example.com" || rule.Subject != "project notes" || rule.MoveTo != "Project Notes" || !rule.MarkRead || rule.Enabled {
		t.Fatalf("saved rule fields do not match form: %+v", rule)
	}
	m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m.rules.inputs[0].SetValue("Unsaved name")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	stored, err = config.LoadMailRules()
	if err != nil || stored[0].Name != "Jack and Jil" || m.rules.editing || m.activePane != PaneContent {
		t.Fatal("Esc did not cancel without changing the saved definition")
	}
}

func TestSettingsRuleToggleReorderAndConfirmedDelete(t *testing.T) {
	m := newRuleSettingsTest(t, []config.MailRule{testMailRule("first"), testMailRule("second")})
	m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if !m.rules.list[0].Enabled {
		t.Fatal("Space did not enable the selected rule")
	}
	m.Update(tea.KeyPressMsg{Code: 'J', Text: "J"})
	if m.rules.list[0].ID != "second" || m.rules.list[1].ID != "first" || m.rules.cursor != 1 {
		t.Fatal("J did not move the selected rule later")
	}
	m.Update(tea.KeyPressMsg{Code: 'K', Text: "K"})
	if m.rules.list[0].ID != "first" || m.rules.cursor != 0 {
		t.Fatal("K did not move the selected rule earlier")
	}
	m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if !m.rules.confirming || len(m.rules.list) != 2 {
		t.Fatal("delete must require confirmation")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.rules.confirming || len(m.rules.list) != 2 || m.activePane != PaneContent {
		t.Fatal("Esc did not cancel deletion in the content pane")
	}
	m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	stored, err := config.LoadMailRules()
	if err != nil || len(stored) != 1 || stored[0].ID != "second" {
		t.Fatalf("confirmed deletion was not persisted: %v", err)
	}
}

func TestSettingsRuleValidationVisibleAndDraftPreserved(t *testing.T) {
	m := newRuleSettingsTest(t, nil)
	m.beginRuleEdit(0)
	m.commitRuleForm()
	if !m.rules.editing || !strings.Contains(m.viewRules(), "Cannot save rule") {
		t.Fatal("invalid rule did not show a visible validation error")
	}
	m.rules.inputs[0].SetValue("Move mail")
	m.rules.inputs[1].SetValue("sender@example.com")
	m.rules.inputs[4].SetValue("INBOX")
	m.commitRuleForm()
	if !m.rules.editing || !strings.Contains(m.rules.errorText, "INBOX") {
		t.Fatal("moving to INBOX was accepted")
	}
	stored, err := config.LoadMailRules()
	if err != nil || len(stored) != 0 {
		t.Fatal("invalid draft reached the definitions file")
	}
}

func TestSettingsRulesBadFileCannotBeOverwritten(t *testing.T) {
	for name, content := range map[string]string{
		"malformed": "not JSON",
		"invalid":   `[{"id":"bad","name":"No conditions or actions"}]`,
		"duplicate": `[{"id":"same","name":"One","from":"a","mark_read":true},{"id":"same","name":"Two","from":"b","mark_read":true}]`,
	} {
		t.Run(name, func(t *testing.T) {
			m := newRuleSettingsTest(t, nil)
			path := ruleTestFile(t)
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			m.loadRules()
			if m.rules.loadError == "" || !strings.Contains(m.viewRules(), "Cannot load rules") {
				t.Fatal("bad file did not show a load error")
			}
			m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
			if m.rules.editing || m.saveRules([]config.MailRule{testMailRule("replacement")}) {
				t.Fatal("bad file allowed a replacement definition")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != content {
				t.Fatal("bad rules file was overwritten")
			}
		})
	}
}

func TestSettingsRulesReadFailureAndExternalChangePreserved(t *testing.T) {
	t.Run("unreadable file", func(t *testing.T) {
		m := newRuleSettingsTest(t, nil)
		path := ruleTestFile(t)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		m.loadRules()
		if m.rules.loadError == "" || m.saveRules([]config.MailRule{testMailRule("replacement")}) {
			t.Fatal("read failure did not block changes")
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Fatal("unreadable rules path was replaced")
		}
	})
	t.Run("external valid change", func(t *testing.T) {
		m := newRuleSettingsTest(t, []config.MailRule{testMailRule("original")})
		external := []config.MailRule{testMailRule("external")}
		if err := config.SaveMailRules(external); err != nil {
			t.Fatal(err)
		}
		external, err := config.LoadMailRules()
		if err != nil {
			t.Fatal(err)
		}
		m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
		if m.rules.errorText == "" || m.rules.list[0].Enabled {
			t.Fatal("external changes were not detected transactionally")
		}
		stored, err := config.LoadMailRules()
		if err != nil || !slices.Equal(stored, external) {
			t.Fatal("external changes were overwritten")
		}
		m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		if !slices.Equal(m.rules.list, external) || m.rules.errorText != "" {
			t.Fatal("r did not reload external changes")
		}
	})
	t.Run("file becomes invalid before save", func(t *testing.T) {
		m := newRuleSettingsTest(t, []config.MailRule{testMailRule("original")})
		path := ruleTestFile(t)
		if err := os.WriteFile(path, []byte("broken externally"), 0o600); err != nil {
			t.Fatal(err)
		}
		m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "broken externally" || m.rules.loadError == "" {
			t.Fatal("invalid external changes were not preserved")
		}
	})
}

func TestSettingsRulesFitAndScroll(t *testing.T) {
	m := newRuleSettingsTest(t, nil)
	for _, size := range [][2]int{{40, 12}, {80, 20}, {120, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.beginRuleEdit(len(m.rules.list))
			for field := range ruleFieldCount {
				m.focusRuleField(field)
				_, body, _, start := m.ruleLayout()
				_, positions := m.ruleRows()
				selected := positions[field]
				if ruleInputIndex(field) >= 0 || field == ruleAccount {
					selected++
				}
				if selected < start || selected >= start+len(body) {
					t.Fatalf("field %d is outside the visible body", field)
				}
				view := m.View().Content
				if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
					t.Fatalf("field %d view exceeds terminal: %dx%d", field, lipgloss.Width(view), lipgloss.Height(view))
				}
			}
			m.closeRuleForm()
			m.rules.list = nil
			for i := range 50 {
				m.rules.list = append(m.rules.list, testMailRule(fmt.Sprintf("rule-%d", i)))
			}
			m.rules.cursor = 49
			_, body, _, start := m.ruleLayout()
			if m.rules.cursor < start || m.rules.cursor >= start+len(body) {
				t.Fatal("long rule list did not scroll to selection")
			}
		})
	}
}

func TestSettingsRulePolicyExplainsFutureOnlyBehavior(t *testing.T) {
	m := newRuleSettingsTest(t, nil)
	m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if !m.rules.help {
		t.Fatal("? did not open policy help")
	}
	policy := strings.Join(strings.Fields(strings.Join(m.ruleHelpLines(), " ")), " ")
	for _, want := range []string{"locally", "(AND)", "first enabled matching rule", "future incoming mail only", "existing inbox messages are left alone", "next background check", "30 seconds"} {
		if !strings.Contains(policy, want) {
			t.Fatalf("policy omits %q", want)
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.rules.help || m.activePane != PaneContent {
		t.Fatal("Esc did not close policy help without leaving Rules")
	}
}

func TestSettingsRuleSaveErrorVisibleAndListUnchanged(t *testing.T) {
	m := newRuleSettingsTest(t, []config.MailRule{testMailRule("original")})
	next := []config.MailRule{testMailRule("invalid/id")}
	if m.saveRules(next) || !strings.Contains(m.viewRules(), "Cannot save rules") {
		t.Fatal("save error was not displayed")
	}
	stored, err := config.LoadMailRules()
	if err != nil || len(stored) != 1 || stored[0].ID != "original" || m.rules.list[0].ID != "original" {
		t.Fatal("failed save changed the displayed or persisted definitions")
	}
}

func TestSettingsRuleUnavailableScopePreserved(t *testing.T) {
	rule := testMailRule("scoped")
	rule.AccountID = "unavailable-account"
	m := newRuleSettingsTest(t, []config.MailRule{rule})
	m.beginRuleEdit(0)
	if !strings.Contains(m.ruleAccountLabel(rule.AccountID), "Unavailable") {
		t.Fatal("unavailable account scope was not indicated")
	}
	m.commitRuleForm()
	stored, err := config.LoadMailRules()
	if err != nil || len(stored) != 1 || stored[0].AccountID != rule.AccountID {
		t.Fatal("editing silently broadened an unavailable account scope")
	}
	m.beginRuleEdit(0)
	m.focusRuleField(ruleAccount)
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.rules.draft.AccountID != "" {
		t.Fatal("explicit account selection did not switch to all accounts")
	}
}

func TestSettingsRulesReloadCanonicalDefinitionsAfterSave(t *testing.T) {
	m := newRuleSettingsTest(t, []config.MailRule{testMailRule("original")})
	m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	stored, err := config.LoadMailRules()
	if err != nil || !slices.Equal(m.rules.list, stored) {
		t.Fatal("successful save did not reload the canonical definitions")
	}
	m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if m.rules.errorText != "" || m.rules.loadError != "" || m.rules.list[0].Enabled {
		t.Fatal("a second local save was incorrectly treated as an external change")
	}
}
