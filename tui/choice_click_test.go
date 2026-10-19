package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestClickingStartMenuItemOpensThatItem(t *testing.T) {
	m := NewChoice()
	wants := []tea.Msg{GoToInboxMsg{}, GoToSendMsg{}, GoToMarketplaceMsg{}, GoToSettingsMsg{}}
	if m.hasSavedDrafts {
		wants = []tea.Msg{GoToInboxMsg{}, GoToSendMsg{}, GoToDraftsMsg{}, GoToMarketplaceMsg{}, GoToSettingsMsg{}}
	}
	for _, warnings := range [][]string{nil, {"x conflicts with y"}} {
		for _, update := range []bool{false, true} {
			m.keybindWarnings = warnings
			m.UpdateAvailable = update
			model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
			m = model.(Choice)
			rows := strings.Split(ansi.Strip(m.View().Content), "\n")
			for i, choice := range m.choices {
				row := -1
				for y, line := range rows {
					if strings.Contains(line, choice) {
						row = y
						break
					}
				}
				if row < 0 {
					t.Fatalf("menu item %q not rendered", choice)
				}
				_, cmd := m.Update(tea.MouseClickMsg{X: 6, Y: row, Button: tea.MouseLeft})
				var got tea.Msg
				if cmd != nil {
					got = cmd()
				}
				if reflect.TypeOf(got) != reflect.TypeOf(wants[i]) {
					t.Errorf("warnings %d, update %t: clicking %q on row %d gave %T", len(warnings), update, choice, row, got)
				}
			}
		}
	}
}
