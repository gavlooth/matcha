package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/internal/vimedit"
)

func typeComposerKeys(composer *Composer, text string) {
	for _, r := range text {
		composer.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func vimComposer(body string) *Composer {
	composer := NewComposer("", "friend@example.net", "Subject", body, true)
	composer.SetSpellcheckOptions(true, true)
	composer.SetVimMode(true)
	composer.setFocusTo(focusBody)
	composer.bodyInput.MoveToBegin()
	return composer
}

func TestComposerVimChangeGreekWordUndoAndRedo(t *testing.T) {
	composer := vimComposer("hello world")
	typeComposerKeys(composer, "ciwγεια")
	composer.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := composer.GetBody(); got != "γεια world" {
		t.Fatalf("changed word = %q", got)
	}
	if composer.confirmingExit || composer.vimEditor.Mode() != vimedit.Normal {
		t.Fatal("Escape left composition instead of entering Normal mode")
	}
	typeComposerKeys(composer, "u")
	if got := composer.GetBody(); got != "hello world" {
		t.Fatalf("undo did not restore entire change: %q", got)
	}
	composer.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if got := composer.GetBody(); got != "γεια world" {
		t.Fatalf("redo did not restore Unicode edit: %q", got)
	}
	composer.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if composer.confirmingExit {
		t.Fatal("Normal-mode Escape opened the exit dialog")
	}
}

func TestComposerVimLeavesHeadersAsRegularInputs(t *testing.T) {
	composer := NewComposer("", "", "", "Body", true)
	composer.SetVimMode(true)
	typeComposerKeys(composer, "hjkl@example.com")
	if got := composer.GetTo(); got != "hjkl@example.com" {
		t.Fatalf("Vim intercepted recipient typing: %q", got)
	}
	composer.setFocusTo(focusSubject)
	typeComposerKeys(composer, "delete with dd")
	if got := composer.GetSubject(); got != "delete with dd" {
		t.Fatalf("Vim intercepted subject typing: %q", got)
	}
}

func TestComposerVimTogglePreservesBodyAndFieldNavigation(t *testing.T) {
	composer := vimComposer("αβ🙂")
	typeComposerKeys(composer, "A!")
	composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if composer.focusIndex != focusSignature || composer.vimEditor.Mode() != vimedit.Normal {
		t.Fatal("Tab did not leave Vim insertion for the signature field")
	}
	if got := composer.GetBody(); got != "αβ🙂!" {
		t.Fatalf("leaving body lost text: %q", got)
	}
	composer.setFocusTo(focusBody)
	composer.Update(tea.KeyPressMsg{Code: tea.KeyF4})
	if composer.vimEditor != nil || composer.GetBody() != "αβ🙂!" {
		t.Fatal("F4 did not disable Vim without changing text")
	}
	typeComposerKeys(composer, "i")
	if got := composer.GetBody(); !strings.Contains(got, "i") {
		t.Fatalf("plain editing did not insert i: %q", got)
	}
	body := composer.GetBody()
	composer.Update(tea.KeyPressMsg{Code: tea.KeyF4})
	if composer.vimEditor == nil || composer.GetBody() != body {
		t.Fatal("re-enabling Vim changed text")
	}
	composer.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	composer.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !composer.confirmingExit {
		t.Fatal("Escape outside body no longer offers draft save/exit")
	}
}

func TestComposerCtrlGTogglesVimWithoutChangingMessage(t *testing.T) {
	composer := NewComposer("", "recipient@example.com", "Subject", "αβ🙂", true)
	composer.setFocusTo(focusBody)
	toggle := tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}
	composer.Update(toggle)
	if composer.vimEditor == nil {
		t.Fatal("Ctrl+G did not enable Vim")
	}
	typeComposerKeys(composer, "A!")
	composer.Update(toggle)
	if composer.vimEditor != nil || composer.GetBody() != "αβ🙂!" {
		t.Fatal("Ctrl+G did not leave Vim insertion without losing text")
	}
	composer.setFocusTo(focusTo)
	composer.Update(toggle)
	if composer.vimEditor == nil || composer.GetTo() != "recipient@example.com" || composer.GetSubject() != "Subject" {
		t.Fatal("Ctrl+G in a header changed message fields instead of toggling Vim")
	}
}

func TestComposerVimExternalBodyReplacementClearsOldUndo(t *testing.T) {
	composer := vimComposer("old text")
	typeComposerKeys(composer, "dd")
	composer.SetBody("External editor: Ελληνικά 🙂")
	typeComposerKeys(composer, "u")
	if got := composer.GetBody(); got != "External editor: Ελληνικά 🙂" {
		t.Fatalf("undo resurrected stale content after external edit: %q", got)
	}
}
