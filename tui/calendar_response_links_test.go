package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
)

const responseTestURL = "https://calendar.google.com/calendar/event?action=RESPOND&eid=test-event&tok=test-token&rst="

func calendarBodyEmail() fetcher.Email {
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nMETHOD:REQUEST\r\nBEGIN:VEVENT\r\nUID:meeting-test\r\nDTSTART:20261028T150000Z\r\nDTEND:20261028T153000Z\r\nSUMMARY:Meeting\r\nORGANIZER:mailto:organizer@example.com\r\nATTENDEE:mailto:me@example.com\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	body := "<p>Invitation details</p><p>Reply for me@example.com</p>"
	for i, label := range []string{"Yes", "No", "Maybe"} {
		body += fmt.Sprintf(`<p>🙂答 <a href="%s%d">%s</a></p>`, responseTestURL, i+1, label)
	}
	return fetcher.Email{
		AccountID: "a", From: "organizer@example.com", To: []string{"me@example.com"}, Subject: "Meeting",
		Body: body, BodyMIMEType: "text/html",
		Attachments: []fetcher.Attachment{{IsCalendarInvite: true, Data: []byte(ics)}},
	}
}

func renderedClick(t *testing.T, content, label string) tea.MouseClickMsg {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(content), "\n") {
		if x := strings.Index(line, label); x >= 0 {
			return tea.MouseClickMsg{X: ansi.StringWidth(line[:x]), Y: y, Button: tea.MouseLeft}
		}
	}
	t.Fatalf("response %q not visible", label)
	return tea.MouseClickMsg{}
}

func checkCalendarBodyLinks(t *testing.T, model tea.Model) {
	t.Helper()
	for i, label := range []string{"Yes", "No", "Maybe"} {
		click := renderedClick(t, model.View().Content, "🙂答 "+label)
		click.X += ansi.StringWidth("🙂答 ")
		_, cmd := model.Update(click)
		if cmd == nil {
			t.Fatalf("clicking %s at %d,%d returned no command", label, click.X, click.Y)
		}
		msg, ok := cmd().(OpenCalendarResponseMsg)
		wantURL := fmt.Sprintf("%s%d", responseTestURL, i+1)
		if !ok || msg.URL != wantURL {
			t.Fatalf("clicking %s did not open its original response link: %#v", label, msg)
		}
	}
}

func TestCalendarBodyResponseLinks(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := NewEmailView(calendarBodyEmail(), width, 65, MailboxInbox, true)
			checkCalendarBodyLinks(t, m)
			m.viewport.SetHeight(8)
			m.viewport.ScrollDown(3)
			checkCalendarBodyLinks(t, m)
			m.Update(RSVPResultMsg{Response: "ACCEPTED"})
			checkCalendarBodyLinks(t, m)
		})
	}
}

func TestCalendarBodyResponseLinksInSplit(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	for _, orientation := range []string{config.SplitPaneHorizontal, config.SplitPaneVertical} {
		t.Run(orientation, func(t *testing.T) {
			fi := NewFolderInbox([]string{"INBOX"}, []config.Account{{ID: "a"}})
			fi.SetSplitOrientation(orientation)
			fi.Update(tea.WindowSizeMsg{Width: 180, Height: 100})
			width := fi.calculatePreviewWidth() - 2
			if fi.isVerticalSplit() {
				width = fi.calculateInboxWidthVertical()
			}
			fi.previewPane = NewEmailViewPreview(calendarBodyEmail(), width, 35, 0, 0, true)
			checkCalendarBodyLinks(t, fi)
			fi.previewPane.viewport.SetHeight(8)
			fi.previewPane.viewport.ScrollDown(3)
			checkCalendarBodyLinks(t, fi)
		})
	}
}

func TestCalendarBodyClicksIgnoreUnrelatedTextAndLinks(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	for _, body := range []string{
		"<p>Yes</p>",
		`<p><a href="https://example.com/calendar/event?rst=1">Yes</a></p>`,
		`<p><a href="https://calendar.google.com.evil.example/calendar/event?rst=1">Yes</a></p>`,
		`<p><a href="https://calendar.google.com/calendar/event?rst=4">Yes</a></p>`,
	} {
		email := calendarBodyEmail()
		email.Body = body
		m := NewEmailView(email, 100, 65, MailboxInbox, true)
		click := renderedClick(t, m.View().Content, "Yes")
		_, cmd := m.Update(click)
		if cmd != nil {
			if msg, ok := cmd().(OpenCalendarResponseMsg); ok {
				t.Fatalf("unrelated click opened calendar response: %#v", msg)
			}
		}
	}
}

func TestHyperlinkHitCells(t *testing.T) {
	for _, terminator := range []string{"\a", "\x1b\\"} {
		text := "🙂答 " + "\x1b]8;id=reply;" + responseTestURL + "1" + terminator + "\x1b[1mYes\x1b[0m\x1b]8;;" + terminator + " plain"
		for x := range 14 {
			want := ""
			if x >= 5 && x < 8 {
				want = responseTestURL + "1"
			}
			if got := hyperlinkAt(text, x, 0); got != want {
				t.Fatalf("cell %d opened %q, want %q", x, got, want)
			}
		}
	}
}
