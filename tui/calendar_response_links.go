package tui

import (
	"net/url"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *EmailView) calendarResponseURLAt(x, y int) string {
	if !m.hasCalendarInvite || m.calendarEvent == nil {
		return ""
	}
	y -= lipgloss.Height(m.renderHeader()) + lipgloss.Height(m.renderCalendarView())
	if x < 0 || x >= m.viewport.Width() || y < 0 || y >= m.viewport.Height() {
		return ""
	}
	link := hyperlinkAt(m.viewport.View(), x, y)
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.Host != "calendar.google.com" || u.User != nil || u.Path != "/calendar/event" {
		return ""
	}
	switch u.Query().Get("rst") {
	case "1", "2", "3":
		return link
	default:
		return ""
	}
}

// hyperlinkAt reads the displayed cells, including wrapping and viewport
// scrolling, rather than searching for response words in arbitrary email text.
func hyperlinkAt(text string, x, y int) string {
	column, row := 0, 0
	var state byte
	var link string
	for len(text) > 0 {
		sequence, width, n, nextState := ansi.DecodeSequence(text, state, nil)
		state = nextState
		text = text[n:]
		switch {
		case sequence == "\n":
			row++
			column = 0
			if row > y {
				return ""
			}
		case strings.HasPrefix(sequence, "\x1b]8;"):
			data := strings.TrimSuffix(strings.TrimSuffix(sequence[4:], "\a"), "\x1b\\")
			_, link, _ = strings.Cut(data, ";")
		case width > 0:
			if row == y && x >= column && x < column+width {
				return link
			}
			column += width
		}
	}
	return ""
}
