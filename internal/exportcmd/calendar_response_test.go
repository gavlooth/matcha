package exportcmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/floatpane/matcha/tui"
)

// The launcher is always a local stub, never a browser or a network request.
func stubCalendarBrowser(t *testing.T, script string) string {
	t.Helper()
	var name string
	switch runtime.GOOS {
	case goosLinux:
		name = "xdg-open"
	case goosDarwin:
		name = "open"
	default:
		t.Skip("browser stub requires a Unix launcher")
	}
	dir := t.TempDir()
	record := filepath.Join(dir, "arguments")
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("MATCHA_CALENDAR_BROWSER_RECORD", record)
	return record
}

func TestOpenCalendarResponseRejectsInvalidURLsWithoutLaunching(t *testing.T) {
	record := stubCalendarBrowser(t, "printf '%s\\n' launched > \"$MATCHA_CALENDAR_BROWSER_RECORD\"\n")
	for _, tc := range []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"http", "http://calendar.google.com/calendar/event?rst=1"},
		{"javascript", "javascript:alert('private-test-token')"},
		{"file", "file:///calendar/event?rst=1"},
		{"custom scheme", "webcal://calendar.google.com/calendar/event?rst=1"},
		{"relative", "/calendar/event?rst=1"},
		{"protocol relative", "//calendar.google.com/calendar/event?rst=1"},
		{"host suffix", "https://calendar.google.com.evil.example/calendar/event?rst=1"},
		{"host prefix", "https://evilcalendar.google.com/calendar/event?rst=1"},
		{"userinfo host confusion", "https://calendar.google.com@evil.example/calendar/event?rst=1"},
		{"userinfo", "https://user:private-test-token@calendar.google.com/calendar/event?rst=1"},
		{"default port", "https://calendar.google.com:443/calendar/event?rst=1"},
		{"custom port", "https://calendar.google.com:8443/calendar/event?rst=1"},
		{"empty port", "https://calendar.google.com:/calendar/event?rst=1"},
		{"trailing dot", "https://calendar.google.com./calendar/event?rst=1"},
		{"wrong path", "https://calendar.google.com/calendar/event/other?rst=1"},
		{"missing response", "https://calendar.google.com/calendar/event?tok=private-test-token"},
		{"fragment response", "https://calendar.google.com/calendar/event#rst=1"},
		{"invalid response", "https://calendar.google.com/calendar/event?rst=4"},
		{"duplicate response", "https://calendar.google.com/calendar/event?rst=1&rst=2"},
		{"malformed query", "https://calendar.google.com/calendar/event?rst=1&tok=%ZZprivate-test-token"},
		{"malformed URL", "https://calendar.google.com/%ZZprivate-test-token?rst=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, ok := OpenCalendarResponseCmd(tc.url)().(tui.CalendarResponseOpenedMsg)
			if !ok || result.Err == nil {
				t.Fatalf("invalid URL did not return a browser error: %#v", result)
			}
			if strings.Contains(result.Err.Error(), "private-test-token") || strings.Contains(result.Err.Error(), tc.url) && tc.url != "" {
				t.Fatalf("error exposes the response URL: %v", result.Err)
			}
			if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid URL started the browser: record error = %v", err)
			}
		})
	}
}

func TestOpenCalendarResponsePassesOriginalURLAsOneArgument(t *testing.T) {
	record := stubCalendarBrowser(t, "printf '%s\\n' \"$#\" \"$@\" > \"$MATCHA_CALENDAR_BROWSER_RECORD\"\n")
	for _, response := range []string{"1", "2", "3"} {
		t.Run(response, func(t *testing.T) {
			responseURL := "https://calendar.google.com/calendar/event?action=RESPOND&ctz=Europe%2FLondon&eid=synthetic&es=0&hl=en&rst=" + response + "&tok=private-test-token%26%3B%24%28value%29"
			result, ok := OpenCalendarResponseCmd(responseURL)().(tui.CalendarResponseOpenedMsg)
			if !ok || result.Err != nil {
				t.Fatalf("browser launch failed: %#v", result)
			}
			got, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "1\n"+responseURL+"\n" {
				t.Fatalf("launcher arguments = %q, want one unchanged URL", got)
			}
		})
	}
}

func TestOpenCalendarResponseReportsLauncherExitFailureWithoutTokens(t *testing.T) {
	stubCalendarBrowser(t, "printf '%s\\n' \"$1\" >&2\nexit 17\n")
	result := OpenCalendarResponseCmd("https://calendar.google.com/calendar/event?rst=1&tok=private-test-token")().(tui.CalendarResponseOpenedMsg)
	if result.Err == nil {
		t.Fatal("launcher exit failure was reported as success")
	}
	if strings.Contains(result.Err.Error(), "private-test-token") || strings.Contains(result.Err.Error(), "https://") {
		t.Fatalf("launcher failure exposes private URL: %v", result.Err)
	}
}

func TestOpenCalendarResponseReportsMissingLauncher(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	result := OpenCalendarResponseCmd("https://calendar.google.com/calendar/event?rst=1&tok=private-test-token")().(tui.CalendarResponseOpenedMsg)
	if result.Err == nil {
		t.Fatal("missing launcher was reported as success")
	}
	if strings.Contains(result.Err.Error(), "private-test-token") {
		t.Fatalf("missing launcher error exposes private URL: %v", result.Err)
	}
}
