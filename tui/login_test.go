package tui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestProtocolComboboxCycles(t *testing.T) {
	m := NewLogin(true)

	// Starts focused on the protocol combobox with the default selection.
	if got := m.protocol(); got != "imap" {
		t.Fatalf("initial protocol = %q, want imap", got)
	}

	right := tea.KeyPressMsg{Code: tea.KeyRight}
	want := []string{"jmap", "pop3", "maildir", "imap"} // wraps around
	for _, w := range want {
		model, _ := m.Update(right)
		m = model.(*Login)
		if got := m.protocol(); got != w {
			t.Fatalf("after right, protocol = %q, want %q", got, w)
		}
	}

	// Left cycles backwards, wrapping from imap to maildir.
	model, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = model.(*Login)
	if got := m.protocol(); got != "maildir" {
		t.Fatalf("after left, protocol = %q, want maildir", got)
	}
}

func TestProtocolComboboxIgnoresTyping(t *testing.T) {
	m := NewLogin(true)
	// Focused on the protocol field; typed characters must not edit it.
	model, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = model.(*Login)
	if got := m.protocol(); got != "imap" {
		t.Fatalf("after typing, protocol = %q, want imap (unchanged)", got)
	}
}

func TestValidPort(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fallback int
		want     int
	}{
		{"empty string returns fallback", "", 993, 993},
		{"valid port 993", "993", 143, 993},
		{"valid port 1 (minimum)", "1", 993, 1},
		{"valid port 65535 (maximum)", "65535", 993, 65535},
		{"valid port 587", "587", 25, 587},
		{"valid port 995", "995", 110, 995},
		{"port 0 is invalid, returns fallback", "0", 993, 993},
		{"negative port is invalid", "-1", 993, 993},
		{"port over 65535 is invalid", "70000", 993, 993},
		{"non-numeric returns fallback", "abc", 993, 993},
		{"port with spaces returns fallback", " 993 ", 993, 993},
		{"port 8080", "8080", 993, 8080},
		{"large negative number", "-99999", 993, 993},
		{"very large number", "9999999", 993, 993},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validPort(tt.input, tt.fallback)
			if got != tt.want {
				t.Errorf("validPort(%q, %d) = %d, want %d", tt.input, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestValidPortDifferentFallbacks(t *testing.T) {
	if got := validPort("", 143); got != 143 {
		t.Errorf("empty with fallback 143 = %d, want 143", got)
	}
	if got := validPort("", 587); got != 587 {
		t.Errorf("empty with fallback 587 = %d, want 587", got)
	}
	if got := validPort("", 995); got != 995 {
		t.Errorf("empty with fallback 995 = %d, want 995", got)
	}
	if got := validPort("bad", 25); got != 25 {
		t.Errorf("invalid with fallback 25 = %d, want 25", got)
	}
}

func TestSubmitFormPortValidation(t *testing.T) {
	m := NewLogin(true)

	tests := []struct {
		name     string
		portVal  string
		want     int
		fallback int
	}{
		{"valid custom port 143", "143", 143, 993},
		{"invalid port 0 falls back", "0", 993, 993},
		{"invalid negative port falls back", "-1", 993, 993},
		{"invalid overflow port falls back", "70000", 993, 993},
		{"non-numeric port falls back", "abc", 993, 993},
		{"empty port uses default", "", 993, 993},
		{"boundary port 1", "1", 1, 993},
		{"boundary port 65535", "65535", 65535, 993},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.inputs[inputIMAPPort].SetValue(tt.portVal)
			fn := m.submitForm()
			msg := fn().(Credentials)
			if msg.IMAPPort != tt.want {
				t.Errorf("IMAPPort for input %q = %d, want %d", tt.portVal, msg.IMAPPort, tt.want)
			}
		})
	}
}

func TestEditingAccountKeepsSignInAndAddressSettings(t *testing.T) {
	for _, account := range []GoToEditAccountMsg{
		{AccountID: "outlook", Provider: "outlook", Email: "me@outlook.com", AuthMethod: "oauth2"},
		// Yahoo is set up as a custom provider that signs in with OAuth2; the
		// form has no auth field for custom providers.
		{AccountID: "yahoo", Provider: "custom", Email: "me@yahoo.gr", AuthMethod: "oauth2", IMAPServer: "imap.mail.yahoo.com", IMAPPort: 993},
	} {
		t.Run(account.AccountID, func(t *testing.T) {
			account.ExtraAddresses = []string{"old@outlook.com", "older@outlook.com"}
			account.AlwaysShowMailFrom = []string{"ee.duth.gr"}
			m := NewLogin(true)
			m.SetEditMode(account)
			got := m.submitForm()().(Credentials)
			if got.AuthMethod != "oauth2" {
				t.Errorf("AuthMethod = %q, want oauth2: saving the form must not turn the account into a password account", got.AuthMethod)
			}
			if !slices.Equal(got.ExtraAddresses, account.ExtraAddresses) || !slices.Equal(got.AlwaysShowMailFrom, account.AlwaysShowMailFrom) {
				t.Errorf("address settings = %v / %v, want %v / %v", got.ExtraAddresses, got.AlwaysShowMailFrom, account.ExtraAddresses, account.AlwaysShowMailFrom)
			}
		})
	}
}

func TestSplitAddressList(t *testing.T) {
	got := splitAddressList(" a@x.com, b@y.com;c.org\td@z.com,, ")
	if want := []string{"a@x.com", "b@y.com", "c.org", "d@z.com"}; !slices.Equal(got, want) {
		t.Errorf("splitAddressList = %q, want %q", got, want)
	}
}
