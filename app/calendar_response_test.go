package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
	"github.com/floatpane/matcha/internal/emailactions"
	"github.com/floatpane/matcha/internal/palette"
	"github.com/floatpane/matcha/tui"
)

func TestCalendarBrowserFeedbackKeepsInvitationVisible(t *testing.T) {
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:browser-test\r\nDTSTART:20261027T120000Z\r\nDTEND:20261027T130000Z\r\nSUMMARY:Meeting\r\nORGANIZER:mailto:organizer@example.com\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	for _, tc := range []struct {
		name    string
		err     error
		visible []string
	}{
		{"success", nil, []string{"ℹ", "Opened calendar response in browser;", "complete it there."}},
		{"failure", errors.New("launch failed"), []string{"✗", "Failed to open calendar response:", "launch failed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := tui.NewEmailView(fetcher.Email{
				Subject: "Calendar invitation", Body: "Keep this invitation open.", BodyMIMEType: "text/plain",
				Attachments: []fetcher.Attachment{{IsCalendarInvite: true, Data: []byte(ics)}},
			}, 120, 40, tui.MailboxInbox, true)
			previous := tui.NewChoice()
			m := &Model{
				current: view, previousModel: &previous, config: &config.Config{},
				palette: palette.New(), actionManager: &emailactions.Manager{}, width: 120, height: 40,
			}
			before := view.View().Content

			m.Update(tui.CalendarResponseOpenedMsg{Err: tc.err})

			if m.current != view || m.previousModel != &previous {
				t.Fatal("browser completion changed the current or previous view")
			}
			if view.View().Content != before {
				t.Fatal("browser completion changed the invitation")
			}
			content := ansi.Strip(m.View().Content)
			for _, text := range tc.visible {
				if !strings.Contains(content, text) {
					t.Fatalf("missing visible browser feedback %q:\n%s", text, content)
				}
			}
			if !strings.Contains(content, "Keep this invitation open.") {
				t.Fatalf("notification hid the invitation body:\n%s", content)
			}
			if strings.Contains(content, "Response sent:") {
				t.Fatal("browser launch was incorrectly shown as a completed RSVP")
			}
		})
	}
}
