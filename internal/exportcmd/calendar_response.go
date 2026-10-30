package exportcmd

import (
	"errors"
	"net/url"
	"os/exec"
	"runtime"

	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/tui"
)

// OpenCalendarResponseCmd opens the original Google Calendar response URL in the
// default browser. It does not send an RSVP; the user completes it in the browser.
func OpenCalendarResponseCmd(responseURL string) tea.Cmd {
	return func() tea.Msg {
		parsed, err := url.Parse(responseURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "calendar.google.com" ||
			parsed.User != nil || parsed.Path != "/calendar/event" {
			return tui.CalendarResponseOpenedMsg{Err: errors.New("invalid Google Calendar response URL")}
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		responses := query["rst"]
		if err != nil || len(responses) != 1 || (responses[0] != "1" && responses[0] != "2" && responses[0] != "3") {
			return tui.CalendarResponseOpenedMsg{Err: errors.New("invalid Google Calendar response URL")}
		}

		var cmd *exec.Cmd
		switch runtime.GOOS {
		case goosDarwin:
			cmd = exec.Command("open", responseURL) //nolint:noctx
		case goosLinux:
			cmd = exec.Command("xdg-open", responseURL) //nolint:noctx
		case goosWindows:
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", responseURL) //nolint:noctx
		default:
			return tui.CalendarResponseOpenedMsg{Err: errors.New("browser opening is unsupported on this operating system")}
		}
		// Bubble Tea runs commands off the UI thread. Run waits and reaps the
		// launcher, and errors deliberately omit the private response URL.
		if err := cmd.Run(); err != nil {
			return tui.CalendarResponseOpenedMsg{Err: errors.New("failed to launch browser")}
		}
		return tui.CalendarResponseOpenedMsg{}
	}
}
