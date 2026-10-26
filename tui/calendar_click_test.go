package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	calendar "github.com/floatpane/go-icalendar"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
)

func TestCalendarResponseClicks(t *testing.T) {
	m := NewEmailView(fetcher.Email{}, 100, 40, MailboxInbox, true)
	m.hasCalendarInvite = true
	m.calendarEvent = &calendar.Event{Summary: "Meeting"}
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for _, tc := range []struct{ label, response string }{{"1:Accept", "ACCEPTED"}, {"2:Decline", "DECLINED"}, {"3:Tentative", "TENTATIVE"}} {
		found := false
		for y, line := range lines {
			x := strings.Index(line, tc.label)
			if x < 0 {
				continue
			}
			found = true
			_, cmd := m.Update(tea.MouseClickMsg{X: ansi.StringWidth(line[:x]), Y: y, Button: tea.MouseLeft})
			if cmd == nil {
				t.Fatalf("%s returned no command", tc.label)
			}
			msg, ok := cmd().(SendRSVPMsg)
			if !ok || msg.Response != tc.response {
				t.Fatalf("%s returned %#v", tc.label, msg)
			}
		}
		if !found {
			t.Fatalf("%s not displayed", tc.label)
		}
	}
}

func TestCalendarResponseClicksInSplit(t *testing.T) {
	for _, orientation := range []string{config.SplitPaneHorizontal, config.SplitPaneVertical} {
		t.Run(orientation, func(t *testing.T) {
			fi := NewFolderInbox([]string{"INBOX"}, []config.Account{{ID: "a"}})
			fi.SetSplitOrientation(orientation)
			fi.Update(tea.WindowSizeMsg{Width: 160, Height: 70})
			width := fi.calculatePreviewWidth() - 2
			if fi.isVerticalSplit() {
				width = fi.calculateInboxWidthVertical()
			}
			fi.previewPane = NewEmailViewPreview(fetcher.Email{}, width, 35, 0, 0, true)
			fi.previewPane.hasCalendarInvite = true
			fi.previewPane.calendarEvent = &calendar.Event{Summary: "Meeting"}
			lines := strings.Split(ansi.Strip(fi.View().Content), "\n")
			for _, tc := range []struct{ label, response string }{{"1:Accept", "ACCEPTED"}, {"2:Decline", "DECLINED"}, {"3:Tentative", "TENTATIVE"}} {
				found := false
				for y, line := range lines {
					x := strings.Index(line, tc.label)
					if x < 0 {
						continue
					}
					found = true
					_, cmd := fi.Update(tea.MouseClickMsg{X: ansi.StringWidth(line[:x]), Y: y, Button: tea.MouseLeft})
					if cmd == nil {
						t.Fatalf("%s at %d,%d returned no command; sidebar width %d inbox width %d", tc.label, ansi.StringWidth(line[:x]), y, lipgloss.Width(fi.renderSidebar()), lipgloss.Width(fi.renderInboxPane()))
					}
					msg, ok := cmd().(SendRSVPMsg)
					if !ok || msg.Response != tc.response {
						t.Fatalf("%s returned %#v", tc.label, msg)
					}
				}
				if !found {
					t.Fatalf("%s not displayed", tc.label)
				}
			}
			fi.Update(RSVPResultMsg{Response: "ACCEPTED"})
			if !strings.Contains(ansi.Strip(fi.View().Content), "Response sent: Accepted") {
				t.Fatal("successful response not visible in preview")
			}
		})
	}
}
