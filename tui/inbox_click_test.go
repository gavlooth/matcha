package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
)

func TestClickingEmailRowSelectsThatEmail(t *testing.T) {
	previousProvider := uiProvider
	t.Cleanup(func() { uiProvider = previousProvider })
	oneAccount := []config.Account{{ID: "a", FetchEmail: "a@example.com"}}
	twoAccounts := []config.Account{{ID: "a", FetchEmail: "a@example.com"}, {ID: "b", FetchEmail: "b@example.com"}}
	cases := []struct {
		name          string
		accounts      []config.Account
		headerVisible bool
		threaded      bool
	}{
		{"one account", oneAccount, true, false},
		{"two accounts", twoAccounts, true, false},
		{"two accounts, header hidden", twoAccounts, false, false},
		{"one account, threaded", oneAccount, true, true},
	}
	subjects := []string{"SubjectOne", "SubjectTwo", "SubjectThree"}
	for _, tc := range cases {
		uiProvider = &stubUIProvider{visibility: map[string]bool{"header": tc.headerVisible}}
		emails := make([]fetcher.Email, len(subjects))
		for i, subject := range subjects {
			emails[i] = fetcher.Email{UID: uint32(i + 1), AccountID: "a", From: "x@example.com", Subject: subject}
		}
		fi := NewFolderInbox([]string{keyINBOX}, tc.accounts)
		fi.inbox.SetDefaultThreaded(tc.threaded)
		fi.SetEmails(emails, tc.accounts)
		fi.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		rows := strings.Split(ansi.Strip(fi.View().Content), "\n")
		for _, subject := range subjects {
			row := -1
			for y, line := range rows {
				if strings.Contains(line, subject) {
					row = y
					break
				}
			}
			if row < 0 {
				t.Fatalf("%s: %q not rendered", tc.name, subject)
			}
			fi.Update(tea.MouseClickMsg{X: 40, Y: row, Button: tea.MouseLeft})
			got, _ := fi.inbox.list.SelectedItem().(item)
			if got.title != subject {
				t.Errorf("%s: clicking %q on row %d selected %q", tc.name, subject, row, got.title)
			}
		}
	}
}
