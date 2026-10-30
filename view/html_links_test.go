package view

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestHTMLLinkQueryEntitiesDecodeOnce(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	for _, tc := range []struct{ name, href, want string }{
		{"named ampersand", "https://calendar.google.com/calendar/event?action=RESPOND&amp;rst=1", "https://calendar.google.com/calendar/event?action=RESPOND&rst=1"},
		{"numeric ampersand", "https://calendar.google.com/calendar/event?action=RESPOND&#38;rst=1", "https://calendar.google.com/calendar/event?action=RESPOND&rst=1"},
		{"literal entity text", "https://example.com/?value=one&amp;amp;two=three", "https://example.com/?value=one&amp;two=three"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			style := lipgloss.NewStyle()
			rendered, _, err := ProcessBody(`<a href="`+tc.href+`">Yes</a>`, "text/html", style, style, style, true)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(rendered, "\x1b]8;;"+tc.want+"\a") {
				t.Fatalf("rendered link changed its query: %q", rendered)
			}
		})
	}
}
