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
	wants := []tea.Msg{
		GoToInboxMsg{},
		GoToInboxMsg{Hidden: true},
		GoToSendMsg{},
	}
	if m.hasSavedDrafts {
		wants = append(wants, GoToDraftsMsg{})
	}
	wants = append(wants, GoToMarketplaceMsg{}, GoToSettingsMsg{})
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
				if !reflect.DeepEqual(got, wants[i]) {
					t.Errorf("warnings %d, update %t: clicking %q on row %d gave %#v, want %#v", len(warnings), update, choice, row, got, wants[i])
				}
			}
		}
	}
}

func TestStartMenuRoutesWithAndWithoutDrafts(t *testing.T) {
	for _, hasDrafts := range []bool{false, true} {
		m := Choice{hasSavedDrafts: hasDrafts}
		wants := []tea.Msg{
			GoToInboxMsg{},
			GoToInboxMsg{Hidden: true},
			GoToSendMsg{},
		}
		if hasDrafts {
			wants = append(wants, GoToDraftsMsg{})
		}
		wants = append(wants, GoToMarketplaceMsg{}, GoToSettingsMsg{})
		for idx, want := range wants {
			m.cursor = idx
			_, cmd := m.handleSelect()
			if cmd == nil {
				t.Fatalf("drafts %t, choice %d: got no route command", hasDrafts, idx)
			}
			if got := cmd(); !reflect.DeepEqual(got, want) {
				t.Errorf("drafts %t, choice %d: route = %#v, want %#v", hasDrafts, idx, got, want)
			}
		}
	}
}

func TestHiddenMailKeyboardRoutes(t *testing.T) {
	m := NewChoice()
	for _, tc := range []struct {
		name string
		msg  tea.Msg
	}{
		{name: "Enter", msg: tea.KeyPressMsg{Code: tea.KeyEnter}},
		{name: "H", msg: tea.KeyPressMsg{Code: 'H', Text: "H"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := m
			if tc.name == "Enter" {
				model.cursor = 1
			}
			_, cmd := model.Update(tc.msg)
			if cmd == nil {
				t.Fatal("hidden-mail route returned no command")
			}
			if got, want := cmd(), (GoToInboxMsg{Hidden: true}); !reflect.DeepEqual(got, want) {
				t.Fatalf("route = %#v, want %#v", got, want)
			}
		})
	}
}
