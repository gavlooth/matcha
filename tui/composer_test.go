package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/floatpane/matcha/config"
)

func TestMailingListSuggestionTruncates(t *testing.T) {
	composer := NewComposer("", "", "", "", false)
	composer.width = 60

	addresses := make([]string, 20)
	for i := range addresses {
		addresses[i] = fmt.Sprintf("very.long.recipient.%02d@example.com", i)
	}

	display := suggestionDisplay(config.Contact{
		Name:      "Team",
		Addresses: addresses,
	}, suggestionDisplayWidth(composer.width))

	if got, want := len([]rune(display)), suggestionDisplayWidth(composer.width); got > want {
		t.Fatalf("Expected mailing-list suggestion to be at most %d runes, got %d: %q", want, got, display)
	}

	singleAddress := config.Contact{
		Name:  "Very Long Contact Name That Should Stay Fully Visible",
		Email: "very.long.single.address.that.exceeds.width@example.com",
	}
	singleDisplay := suggestionDisplay(singleAddress, suggestionDisplayWidth(composer.width))
	expected := fmt.Sprintf("%s <%s>", singleAddress.Name, singleAddress.Email)
	if singleDisplay != expected {
		t.Fatalf("Expected single-address suggestion to stay untruncated, got %q", singleDisplay)
	}
}

func TestNormalizeEmailList(t *testing.T) {
	got, ok := normalizeEmailList("Alice Example <alice@example.com>, bob@example.com")
	if !ok {
		t.Fatal("Expected valid email list")
	}
	if want := "alice@example.com, bob@example.com"; got != want {
		t.Fatalf("normalizeEmailList() = %q, want %q", got, want)
	}

	if _, ok := normalizeEmailList("not-an-email"); ok {
		t.Fatal("Expected invalid email list")
	}
}

func TestComposerEmailValidationOnFieldBlur(t *testing.T) {
	composer := NewComposer("", "", "", "", false)
	composer.toInput.SetValue("not-an-email")

	model, _ := composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	composer = model.(*Composer)

	if composer.toError == "" {
		t.Fatal("Expected To validation error after leaving invalid field")
	}
	if !strings.Contains(fmt.Sprint(composer.View()), composer.toError) {
		t.Fatal("Expected validation error to be rendered below To field")
	}
}

func TestComposerFromValidationOnFieldBlur(t *testing.T) {
	tests := []struct {
		name      string
		from      string
		wantError bool
	}{
		{
			name:      "invalid from",
			from:      "not-an-email",
			wantError: true,
		},
		{
			name: "bare address",
			from: "user@example.org",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accounts := []config.Account{
				{ID: "account-1", Email: "user@example.org", CatchAll: true},
			}
			composer := NewComposerWithAccounts(accounts, "account-1", "", "", "", false)
			composer.focusIndex = focusFrom
			composer.fromInput.Focus()
			composer.fromInput.SetValue(tt.from)

			model, _ := composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			composer = model.(*Composer)

			if tt.wantError {
				if composer.fromError == "" {
					t.Fatal("Expected From validation error after leaving invalid catch-all From field")
				}
				if !strings.Contains(fmt.Sprint(composer.View()), composer.fromError) {
					t.Fatal("Expected From validation error to be rendered below From field")
				}
				return
			}
			if composer.fromError != "" {
				t.Fatalf("Expected From address to be valid, got %q", composer.fromError)
			}
		})
	}
}

func TestComposerEmailValidationClearsWhenTyping(t *testing.T) {
	composer := NewComposer("", "", "", "", false)
	composer.toInput.SetValue("not-an-email")

	model, _ := composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	composer = model.(*Composer)
	if composer.toError == "" {
		t.Fatal("Expected To validation error after leaving invalid field")
	}

	composer.focusIndex = focusTo
	composer.toInput.Focus()
	model, _ = composer.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	composer = model.(*Composer)

	if composer.toError != "" {
		t.Fatalf("Expected To validation error to clear when typing, got %q", composer.toError)
	}
}

func TestComposerSendValidatesEmailFields(t *testing.T) {
	tests := []struct {
		name          string
		to            string
		cc            string
		catchAllFrom  string
		wantCcError   bool
		wantFromError bool
	}{
		{
			name:        "invalid cc",
			to:          "recipient@example.com",
			cc:          "not-an-email",
			wantCcError: true,
		},
		{
			name:          "invalid catch-all from",
			to:            "recipient@example.com",
			catchAllFrom:  "not-an-email",
			wantFromError: true,
		},
		{
			name: "no recipients",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var composer *Composer
			if tt.catchAllFrom != "" {
				accounts := []config.Account{
					{ID: "account-1", Email: "user@example.org", CatchAll: true},
				}
				composer = NewComposerWithAccounts(accounts, "account-1", "", "", "", false)
				composer.fromInput.SetValue(tt.catchAllFrom)
			} else {
				composer = NewComposer("", "", "", "", false)
			}
			composer.toInput.SetValue(tt.to)
			composer.ccInput.SetValue(tt.cc)
			composer.subjectInput.SetValue("Test Subject")
			composer.bodyInput.SetValue("This is the body.")
			composer.focusIndex = focusSend

			model, cmd := composer.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			composer = model.(*Composer)

			if cmd == nil {
				t.Fatal("Expected a NotifyMsg command after failed send attempt")
			}
			msg := cmd()
			if _, ok := msg.(NotifyMsg); !ok {
				t.Fatalf("Expected NotifyMsg from failed send, got %T", msg)
			}
			if tt.wantCcError && composer.ccError == "" {
				t.Fatal("Expected Cc validation error after send attempt")
			}
			if tt.wantFromError && composer.fromError == "" {
				t.Fatal("Expected From validation error after send attempt")
			}
			if tt.wantCcError && !strings.Contains(fmt.Sprint(composer.View()), composer.ccError) {
				t.Fatal("Expected Cc validation error to be rendered in composer view")
			}
			if tt.wantFromError && !strings.Contains(fmt.Sprint(composer.View()), composer.fromError) {
				t.Fatal("Expected From validation error to be rendered in composer view")
			}
		})
	}
}

func TestComposerContactSuggestionUsesDisplayName(t *testing.T) {
	composer := NewComposer("", "", "", "", false)
	composer.showSuggestions = true
	composer.suggestions = []config.Contact{{
		Name:  "Alice Example",
		Email: "alice@example.com",
	}}

	model, _ := composer.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	composer = model.(*Composer)

	if got, want := composer.toInput.Value(), "Alice Example <alice@example.com>, "; got != want {
		t.Fatalf("Expected suggestion to insert display-name address, got %q, want %q", got, want)
	}
}

// TestComposerUpdate verifies the state transitions in the email composer.
func TestComposerUpdate(t *testing.T) {
	// Initialize a new composer with accounts.
	accounts := []config.Account{
		{ID: "account-1", Email: "test@example.com", Name: "Test User"},
	}
	composer := NewComposerWithAccounts(accounts, "account-1", "", "", "", false)

	t.Run("Focus cycling", func(t *testing.T) {
		// Initial focus is on the 'To' input (index 1, since From is 0).
		// But NewComposer starts focus at focusTo which is 1.
		if composer.focusIndex != focusTo {
			t.Errorf("Initial focusIndex should be %d (focusTo), got %d", focusTo, composer.focusIndex)
		}

		// Simulate pressing Tab to move to the 'Cc' field.
		model, _ := composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		composer = model.(*Composer)
		if composer.focusIndex != focusCc {
			t.Errorf("After one Tab, focusIndex should be %d (focusCc), got %d", focusCc, composer.focusIndex)
		}

		// Simulate pressing Tab to move to the 'Bcc' field.
		model, _ = composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		composer = model.(*Composer)
		if composer.focusIndex != focusBcc {
			t.Errorf("After two Tabs, focusIndex should be %d (focusBcc), got %d", focusBcc, composer.focusIndex)
		}

		// Simulate pressing Tab to move to the 'Subject' field.
		model, _ = composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		composer = model.(*Composer)
		if composer.focusIndex != focusSubject {
			t.Errorf("After three Tabs, focusIndex should be %d (focusSubject), got %d", focusSubject, composer.focusIndex)
		}

		// Simulate pressing Tab again to move to the 'Body' field.
		model, _ = composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		composer = model.(*Composer)
		if composer.focusIndex != focusBody {
			t.Errorf("After four Tabs, focusIndex should be %d (focusBody), got %d", focusBody, composer.focusIndex)
		}

		// Simulate pressing Tab again to move to the 'Signature' field.
		model, _ = composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		composer = model.(*Composer)
		if composer.focusIndex != focusSignature {
			t.Errorf("After five Tabs, focusIndex should be %d (focusSignature), got %d", focusSignature, composer.focusIndex)
		}

		// Simulate pressing Tab again to move to the 'Attachment' field.
		model, _ = composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		composer = model.(*Composer)
		if composer.focusIndex != focusAttachment {
			t.Errorf("After six Tabs, focusIndex should be %d (focusAttachment), got %d", focusAttachment, composer.focusIndex)
		}

		// Simulate pressing Tab again to move to the 'Send' button.
		// Crypto toggles are skipped when S/MIME and PGP are not configured.
		model, _ = composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		composer = model.(*Composer)
		if composer.focusIndex != focusSend {
			t.Errorf("After seven Tabs, focusIndex should be %d (focusSend), got %d", focusSend, composer.focusIndex)
		}

		model, _ = composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		composer = model.(*Composer)
		if composer.focusIndex != focusVim {
			t.Fatalf("Tab after Send did not select Vim: %d", composer.focusIndex)
		}
		// Simulate one more Tab to wrap around.
		// With single account, From field is skipped, so it wraps to focusTo.
		model, _ = composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		composer = model.(*Composer)
		if composer.focusIndex != focusTo {
			t.Errorf("Tab after Vim should return to recipients, got focus %d", composer.focusIndex)
		}
	})

	t.Run("Send email message", func(t *testing.T) {
		// Re-initialize composer for this test
		composer = NewComposerWithAccounts(accounts, "account-1", "", "", "", false)

		// Set values for the email fields.
		composer.toInput.SetValue("recipient@example.com")
		composer.subjectInput.SetValue("Test Subject")
		composer.bodyInput.SetValue("This is the body.")
		// Set focus to the Send button.
		composer.focusIndex = focusSend

		// Simulate pressing Enter to send the email.
		_, cmd := composer.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("Expected a command to be returned, but got nil.")
		}

		// Execute the command and check the resulting message.
		msg := cmd()
		sendMsg, ok := msg.(SendEmailMsg)
		if !ok {
			t.Fatalf("Expected a SendEmailMsg, but got %T", msg)
		}

		// Verify the content of the message.
		if sendMsg.To != "recipient@example.com" {
			t.Errorf("Expected To 'recipient@example.com', got %q", sendMsg.To)
		}
		if sendMsg.Subject != "Test Subject" {
			t.Errorf("Expected Subject 'Test Subject', got %q", sendMsg.Subject)
		}
		if sendMsg.Body != "This is the body." {
			t.Errorf("Expected Body 'This is the body.', got %q", sendMsg.Body)
		}
		if sendMsg.AccountID != "account-1" {
			t.Errorf("Expected AccountID 'account-1', got %q", sendMsg.AccountID)
		}
	})

	t.Run("Account picker with multiple accounts", func(t *testing.T) {
		multiAccounts := []config.Account{
			{ID: "account-1", Email: "test1@example.com", Name: "User 1"},
			{ID: "account-2", Email: "test2@example.com", Name: "User 2"},
		}
		multiComposer := NewComposerWithAccounts(multiAccounts, "account-1", "", "", "", false)

		// Move focus to From field
		multiComposer.focusIndex = focusFrom

		// Press Enter to open account picker
		model, _ := multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		multiComposer = model.(*Composer)

		if !multiComposer.showAccountPicker {
			t.Error("Expected account picker to be shown")
		}

		// Navigate down to select second account
		model, _ = multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		multiComposer = model.(*Composer)

		if multiComposer.selectedAccountIdx != 1 {
			t.Errorf("Expected selectedAccountIdx to be 1, got %d", multiComposer.selectedAccountIdx)
		}

		// Press Enter to confirm selection
		model, _ = multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		multiComposer = model.(*Composer)

		if multiComposer.showAccountPicker {
			t.Error("Expected account picker to be closed")
		}

		// Verify the selected account
		if multiComposer.GetSelectedAccountID() != "account-2" {
			t.Errorf("Expected selected account ID 'account-2', got %q", multiComposer.GetSelectedAccountID())
		}
	})

	t.Run("Single account no picker", func(t *testing.T) {
		singleAccounts := []config.Account{
			{ID: "account-1", Email: "test@example.com"},
		}
		singleComposer := NewComposerWithAccounts(singleAccounts, "account-1", "", "", "", false)

		// Move focus to From field
		singleComposer.focusIndex = focusFrom

		// Press Enter - should not open picker with single account
		model, _ := singleComposer.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		singleComposer = model.(*Composer)

		if singleComposer.showAccountPicker {
			t.Error("Account picker should not open with single account")
		}
	})

	t.Run("Multi-account focus cycling includes From", func(t *testing.T) {
		multiAccounts := []config.Account{
			{ID: "account-1", Email: "test1@example.com"},
			{ID: "account-2", Email: "test2@example.com"},
		}
		multiComposer := NewComposerWithAccounts(multiAccounts, "account-1", "", "", "", false)

		// Initial focus is on 'To' field
		if multiComposer.focusIndex != focusTo {
			t.Errorf("Initial focusIndex should be %d (focusTo), got %d", focusTo, multiComposer.focusIndex)
		}

		// Crypto toggles are skipped without configured keys or certificates.
		model, _ := multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // To -> Cc
		multiComposer = model.(*Composer)
		model, _ = multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // Cc -> Bcc
		multiComposer = model.(*Composer)
		model, _ = multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // Bcc -> Subject
		multiComposer = model.(*Composer)
		model, _ = multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // Subject -> Body
		multiComposer = model.(*Composer)
		model, _ = multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // Body -> Signature
		multiComposer = model.(*Composer)
		model, _ = multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // Signature -> Attachment
		multiComposer = model.(*Composer)
		model, _ = multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // Attachment -> Send
		multiComposer = model.(*Composer)
		model, _ = multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // Send -> Vim
		multiComposer = model.(*Composer)
		model, _ = multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // Vim -> From
		multiComposer = model.(*Composer)
		model, _ = multiComposer.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // From -> To (wrap)
		multiComposer = model.(*Composer)

		// With multiple accounts, From field should be included in tab order
		if multiComposer.focusIndex != focusTo {
			t.Errorf("Tab cycle should include From and return to recipients, got focus %d", multiComposer.focusIndex)
		}
	})
}

func TestFormatAttachmentNameIncludesSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.jpg")
	if err := os.WriteFile(path, make([]byte, 1258291), 0600); err != nil {
		t.Fatal(err)
	}

	got := formatAttachmentName(path)
	want := "image.jpg (1.2 MB)"
	if got != want {
		t.Fatalf("formatAttachmentName() = %q, want %q", got, want)
	}
}

func TestFormatAttachmentNameMissingFile(t *testing.T) {
	got := formatAttachmentName("/missing/image.jpg")
	want := "image.jpg"
	if got != want {
		t.Fatalf("formatAttachmentName() = %q, want %q", got, want)
	}
}

func TestComposerAttachmentSelectionAndRemoval(t *testing.T) {
	composer := NewComposer("", "", "", "", false)
	composer.focusIndex = focusAttachment
	composer.attachmentPaths = []string{"/tmp/a.txt", "/tmp/b.txt", "/tmp/c.txt"}
	composer.attachmentNames = map[string]string{
		"/tmp/a.txt": "a.txt",
		"/tmp/b.txt": "b.txt",
		"/tmp/c.txt": "c.txt",
	}

	model, _ := composer.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	composer = model.(*Composer)
	if composer.attachmentCursor != 1 {
		t.Fatalf("Expected attachmentCursor 1 after Down, got %d", composer.attachmentCursor)
	}

	model, _ = composer.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	composer = model.(*Composer)

	want := []string{"/tmp/a.txt", "/tmp/c.txt"}
	if len(composer.attachmentPaths) != len(want) {
		t.Fatalf("Expected %d attachments after removal, got %d", len(want), len(composer.attachmentPaths))
	}
	for i, path := range want {
		if composer.attachmentPaths[i] != path {
			t.Fatalf("attachmentPaths[%d] = %q, want %q", i, composer.attachmentPaths[i], path)
		}
	}
	if _, ok := composer.attachmentNames["/tmp/b.txt"]; ok {
		t.Fatal("Expected removed attachment display name to be deleted")
	}
	if composer.attachmentCursor != 1 {
		t.Fatalf("Expected cursor to stay on the next attachment, got %d", composer.attachmentCursor)
	}

	model, _ = composer.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	composer = model.(*Composer)
	if composer.attachmentCursor != 0 {
		t.Fatalf("Expected attachmentCursor to wrap to 0 after Down, got %d", composer.attachmentCursor)
	}
}

// TestComposerGetFromAddress verifies the from address formatting.
func TestComposerGetFromAddress(t *testing.T) {
	t.Run("With name", func(t *testing.T) {
		accounts := []config.Account{
			{ID: "account-1", FetchEmail: "test@example.com", Name: "Test User"},
		}
		composer := NewComposerWithAccounts(accounts, "account-1", "", "", "", false)

		fromAddr := composer.getFromAddress()
		expected := "Test User <test@example.com>"
		if fromAddr != expected {
			t.Errorf("Expected from address %q, got %q", expected, fromAddr)
		}
	})

	t.Run("Without name", func(t *testing.T) {
		accounts := []config.Account{
			{ID: "account-1", FetchEmail: "test@example.com"},
		}
		composer := NewComposerWithAccounts(accounts, "account-1", "", "", "", false)

		fromAddr := composer.getFromAddress()
		expected := "test@example.com"
		if fromAddr != expected {
			t.Errorf("Expected from address %q, got %q", expected, fromAddr)
		}
	})

	t.Run("Send as overrides fetch email", func(t *testing.T) {
		accounts := []config.Account{
			{ID: "account-1", FetchEmail: "gmail@gmail.com", SendAsEmail: "alias@example.com", Name: "Test User"},
		}
		composer := NewComposerWithAccounts(accounts, "account-1", "", "", "", false)

		fromAddr := composer.getFromAddress()
		expected := "Test User <alias@example.com>"
		if fromAddr != expected {
			t.Errorf("Expected from address %q, got %q", expected, fromAddr)
		}
	})

	t.Run("No accounts", func(t *testing.T) {
		composer := NewComposer("", "", "", "", false)

		fromAddr := composer.getFromAddress()
		if fromAddr != "" {
			t.Errorf("Expected empty from address, got %q", fromAddr)
		}
	})
}

// TestComposerSetSelectedAccount verifies account selection.
func TestComposerSetSelectedAccount(t *testing.T) {
	accounts := []config.Account{
		{ID: "account-1", FetchEmail: "test1@example.com"},
		{ID: "account-2", FetchEmail: "test2@example.com"},
		{ID: "account-3", FetchEmail: "test3@example.com"},
	}
	composer := NewComposerWithAccounts(accounts, "account-1", "", "", "", false)

	composer.SetSelectedAccount("account-3")
	if composer.selectedAccountIdx != 2 {
		t.Errorf("Expected selectedAccountIdx 2, got %d", composer.selectedAccountIdx)
	}
	if composer.GetSelectedAccountID() != "account-3" {
		t.Errorf("Expected selected account ID 'account-3', got %q", composer.GetSelectedAccountID())
	}

	// Test non-existent account (should not change)
	composer.SetSelectedAccount("non-existent")
	if composer.selectedAccountIdx != 2 {
		t.Errorf("Expected selectedAccountIdx to remain 2, got %d", composer.selectedAccountIdx)
	}
}

// TestComposerDynamicHeight verifies that window resize updates textarea heights.
func TestComposerDynamicHeight(t *testing.T) {
	composer := NewComposer("", "", "", "", false)

	model, _ := composer.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	composer = model.(*Composer)

	if composer.height != 40 {
		t.Errorf("Expected height 40, got %d", composer.height)
	}

	bodyH := composer.bodyInput.Height()
	sigH := composer.signatureInput.Height()

	if bodyH <= 3 {
		t.Errorf("Expected bodyInput height > 3, got %d", bodyH)
	}
	if sigH <= 1 {
		t.Errorf("Expected signatureInput height > 1, got %d", sigH)
	}
	if bodyH <= sigH {
		t.Errorf("Expected bodyInput height (%d) > signatureInput height (%d)", bodyH, sigH)
	}

	// Small window: heights should not go below minimums
	model, _ = composer.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	composer = model.(*Composer)
	if composer.bodyInput.Height() < 3 {
		t.Errorf("bodyInput height should be at least 3, got %d", composer.bodyInput.Height())
	}
	if composer.signatureInput.Height() < 2 {
		t.Errorf("signatureInput height should be at least 2, got %d", composer.signatureInput.Height())
	}
}

func TestComposerAliasFromOverridePersistsAndClearsOnAccountSwitch(t *testing.T) {
	accounts := []config.Account{
		{ID: "one", FetchEmail: "main@example.com", Name: "User", ExtraAddresses: []string{"alias@example.com"}},
		{ID: "two", FetchEmail: "other@example.com"},
	}
	composer := NewComposerWithAccounts(accounts, "one", "sender@example.net", "Re: subject", "", false)
	composer.SetFromOverride("User <alias@example.com>")
	if got := composer.ToDraft().FromOverride; got != "User <alias@example.com>" {
		t.Fatalf("draft From override = %q", got)
	}
	restored := NewComposerFromDraft(composer.ToDraft(), accounts, false)
	if !restored.fromOverrideSet || restored.fromInput.Value() != "User <alias@example.com>" || !strings.Contains(fmt.Sprint(restored.View()), "alias@example.com") {
		t.Fatalf("restored From override = %q, set=%v", restored.fromInput.Value(), restored.fromOverrideSet)
	}
	restored.showAccountPicker = true
	next, _ := restored.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	restored = next.(*Composer)
	restored.showAccountPicker = false
	if restored.fromOverrideSet || restored.fromInput.Value() != accounts[1].FormatFromHeader() {
		t.Fatalf("account switch retained alias From %q", restored.fromInput.Value())
	}

	restored.SetSelectedAccount("one")
	restored.SetFromOverride("User <alias@example.com>")
	restored.toInput.SetValue("recipient@example.net")
	restored.focusIndex = focusSend
	model, cmd := restored.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("send command is nil")
	}
	msg, ok := cmd().(SendEmailMsg)
	if !ok {
		t.Fatalf("send command returned %T", cmd())
	}
	if msg.FromOverride != "User <alias@example.com>" {
		t.Fatalf("sent From override = %q", msg.FromOverride)
	}
	_ = model
}

func composerSendClick(test *testing.T, composer *Composer) tea.MouseClickMsg {
	test.Helper()
	label := "[ " + t("composer.send") + " ]"
	for y, line := range strings.Split(ansi.Strip(composer.View().Content), "\n") {
		if x := strings.Index(line, label); x >= 0 {
			return tea.MouseClickMsg{X: ansi.StringWidth(line[:x]), Y: y, Button: tea.MouseLeft}
		}
	}
	test.Fatal("Send button not rendered")
	return tea.MouseClickMsg{}
}

func TestComposerVisibleVimToggle(t *testing.T) {
	for _, width := range []int{80, 110} {
		composer := NewComposer("", "recipient@example.com", "Subject", "αβ🙂", true)
		composer.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		sendClick := composerSendClick(t, composer)
		line := strings.Split(ansi.Strip(composer.View().Content), "\n")[sendClick.Y]
		x := strings.Index(line, "[ Vim: off ]")
		if x < 0 || x+len("[ Vim: off ]") > width {
			t.Fatalf("Vim toggle not visible beside Send at width %d: %q", width, line)
		}
		composer.Update(tea.MouseClickMsg{X: x + 2, Y: sendClick.Y, Button: tea.MouseLeft})
		if composer.vimEditor == nil || composer.focusIndex != focusVim {
			t.Fatal("click did not select and enable Vim")
		}
		if !strings.Contains(ansi.Strip(composer.View().Content), "[ Vim: on ]") {
			t.Fatal("Vim toggle did not show its enabled state")
		}
		composer.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if composer.vimEditor != nil {
			t.Fatal("Enter did not disable Vim")
		}
		composer.setFocusTo(focusSend)
		composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		if composer.focusIndex != focusVim {
			t.Fatal("Tab did not select the Vim toggle after Send")
		}
		composer.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
		if composer.vimEditor == nil || composer.GetBody() != "αβ🙂" ||
			composer.GetTo() != "recipient@example.com" || composer.GetSubject() != "Subject" || composer.sendRequested {
			t.Fatal("Space did not enable Vim while preserving the unsent message")
		}
	}
}

func TestComposerClickSendMatchesKeyboard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, layout := range []string{"default", "hidden cc bcc", "expanded fields"} {
		t.Run(layout, func(t *testing.T) {
			newComposer := func() *Composer {
				accounts := []config.Account{
					{ID: "first", Email: "first@example.com"},
					{ID: "selected", Email: "sender@example.com", CatchAll: true},
				}
				composer := NewComposerWithAccounts(accounts, "selected", "Recipient <recipient@example.com>", "Subject", "Body", false)
				composer.Update(tea.WindowSizeMsg{Width: 100, Height: 45})
				composer.fromInput.SetValue("Alias <alias@example.com>")
				composer.ccInput.SetValue("cc@example.com")
				composer.bccInput.SetValue("bcc@example.com")
				composer.signatureInput.SetValue("Signature")
				composer.quotedText = "\n> Quoted text"
				composer.inReplyTo = "<original@example.com>"
				composer.references = []string{"<thread@example.com>"}
				if layout == "hidden cc bcc" {
					composer.showCcBcc = false
				}
				if layout == "expanded fields" {
					composer.fromError = "Previous From error"
					composer.toError = "Previous To error"
					composer.ccError = "Previous Cc error"
					composer.bccError = "Previous Bcc error"
					composer.showSuggestions = true
					composer.suggestions = []config.Contact{{Name: "Recipient", Email: "recipient@example.com"}}
					composer.attachmentPaths = []string{"/attachment-one.txt", "/attachment-two.txt"}
					composer.accounts[1].SMIMECert = "/certificate.pem"
					composer.accounts[1].SMIMEKey = "/key.pem"
					composer.accounts[1].PGPKeySource = "file"
					composer.pgpKeysAvailable["recipient@example.com"] = true
					composer.pgpKeysAvailable["Recipient <recipient@example.com>"] = true
					composer.signSMIME = true
					composer.encryptSMIME = true
					composer.signPGP = true
					composer.encryptPGP = true
				}
				return composer
			}
			clicked := newComposer()
			click := composerSendClick(t, clicked)
			_, clickCmd := clicked.Update(click)
			if clickCmd == nil {
				t.Fatal("clicking rendered Send returned no command")
			}
			clickMsg, ok := clickCmd().(SendEmailMsg)
			if !ok {
				t.Fatal("clicking rendered Send did not request delivery")
			}
			keyboard := newComposer()
			keyboard.setFocusTo(focusSend)
			_, keyCmd := keyboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if keyCmd == nil {
				t.Fatal("keyboard Send returned no command")
			}
			keyMsg, ok := keyCmd().(SendEmailMsg)
			if !ok || !reflect.DeepEqual(clickMsg, keyMsg) {
				t.Fatalf("click sent %#v, keyboard sent %#v", clickMsg, keyMsg)
			}
			if clickMsg.AccountID != "selected" || clickMsg.FromOverride != "Alias <alias@example.com>" || clickMsg.To != "recipient@example.com" {
				t.Fatalf("click lost selected account or normalized addresses: %#v", clickMsg)
			}
			if clicked.focusIndex != focusSend || clicked.toInput.Focused() || clicked.bodyInput.Focused() {
				t.Fatal("clicking Send did not move focus away from editors")
			}
		})
	}
}

func TestComposerClickSendValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, invalid := range []string{"from", "to", "cc", "bcc", "no recipients"} {
		t.Run(invalid, func(t *testing.T) {
			composer := NewComposerWithAccounts([]config.Account{{ID: "account", Email: "sender@example.com", CatchAll: true}}, "account", "recipient@example.com", "Subject", "Body", false)
			composer.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
			switch invalid {
			case "from":
				composer.fromInput.SetValue("not-an-address")
			case "to":
				composer.toInput.SetValue("not-an-address")
			case "cc":
				composer.ccInput.SetValue("not-an-address")
			case "bcc":
				composer.bccInput.SetValue("not-an-address")
			case "no recipients":
				composer.toInput.SetValue("")
			}
			_, cmd := composer.Update(composerSendClick(t, composer))
			if cmd == nil {
				t.Fatal("invalid click returned no error notification")
			}
			msg := cmd()
			notice, ok := msg.(NotifyMsg)
			if !ok || notice.Message == "" {
				t.Fatalf("invalid click returned %#v, want error notification", msg)
			}
			errors := map[string]string{"from": composer.fromError, "to": composer.toError, "cc": composer.ccError, "bcc": composer.bccError}
			if invalid != "no recipients" {
				if fieldError := errors[invalid]; fieldError == "" || !strings.Contains(composer.View().Content, fieldError) {
					t.Fatal("invalid address error is not visible in composer")
				}
			}
			composer.fromInput.SetValue("sender@example.com")
			composer.toInput.SetValue("recipient@example.com")
			composer.ccInput.SetValue("")
			composer.bccInput.SetValue("")
			_, cmd = composer.Update(composerSendClick(t, composer))
			if cmd == nil {
				t.Fatal("corrected fields cannot be sent")
			}
			if msg := cmd(); reflect.TypeOf(msg) != reflect.TypeOf(SendEmailMsg{}) {
				t.Fatalf("corrected fields returned %T, want SendEmailMsg", msg)
			}
		})
	}
}

func TestComposerSendClickBoundsAndOverlays(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, blocked := range []string{"right click", "left of button", "right of button", "below button", "exit", "accounts", "plugin prompt", "missing keys"} {
		t.Run(blocked, func(t *testing.T) {
			composer := NewComposer("", "recipient@example.com", "Subject", "Body", false)
			click := composerSendClick(t, composer)
			switch blocked {
			case "right click":
				click.Button = tea.MouseRight
			case "left of button":
				click.X = -1
			case "right of button":
				click.X += composer.sendButtonWidth
			case "below button":
				click.Y++
			case "exit":
				composer.confirmingExit = true
			case "accounts":
				composer.showAccountPicker = true
			case "plugin prompt":
				composer.showPluginPrompt = true
			case "missing keys":
				composer.wkdConfirmRecipients = []string{"recipient@example.com"}
			}
			if _, cmd := composer.Update(click); cmd != nil {
				t.Fatal("click outside active Send button returned a command")
			}
		})
	}
}

func TestComposerSendOnlyOnceUntilRestored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, first := range []string{"click", "keyboard"} {
		t.Run(first, func(t *testing.T) {
			composer := NewComposer("", "recipient@example.com", "Subject", "Body", false)
			click := composerSendClick(t, composer)
			var activation tea.Msg = click
			if first == "keyboard" {
				composer.setFocusTo(focusSend)
				activation = tea.KeyPressMsg{Code: tea.KeyEnter}
			}
			_, send := composer.Update(activation)
			if send == nil {
				t.Fatal("first activation returned no send command")
			}
			for _, duplicate := range []tea.Msg{click, tea.MouseReleaseMsg{X: click.X, Y: click.Y, Button: tea.MouseLeft}, tea.KeyPressMsg{Code: tea.KeyEnter}} {
				if _, cmd := composer.Update(duplicate); cmd != nil {
					t.Fatalf("duplicate activation %T returned another command", duplicate)
				}
			}
			if _, ok := send().(SendEmailMsg); !ok {
				t.Fatal("first activation did not emit SendEmailMsg")
			}
			composer.Init()
			_, retry := composer.Update(composerSendClick(t, composer))
			if retry == nil {
				t.Fatal("restored composer cannot send again after Undo")
			}
			if _, ok := retry().(SendEmailMsg); !ok {
				t.Fatal("restored composer did not emit SendEmailMsg")
			}
		})
	}
}
