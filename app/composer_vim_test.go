package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/tui"
)

func appWithVimComposer(t *testing.T) (*Model, *tui.Composer) {
	t.Helper()
	cfg := &config.Config{
		Accounts:        []config.Account{{ID: "a", Email: "me@example.com"}},
		ComposerVimMode: true, DisableSpellcheck: true, HasSeenSetupGuide: true,
	}
	model := NewModel(cfg, nil, nil, false, nil, nil)
	model.Update(tui.GoToSendMsg{To: "friend@example.net", Subject: "Subject", Body: "hello"})
	composer := getCurrentComposer(model.current)
	for i := 0; i < 10 && !composer.IsVimEditing(); i++ {
		model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	if !composer.IsVimEditing() {
		t.Fatal("could not reach Vim body editing")
	}
	return model, composer
}

func TestComposerVimUndoDoesNotCancelQueuedEmail(t *testing.T) {
	model, composer := appWithVimComposer(t)
	for _, r := range "gg0iX" {
		model.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := composer.GetBody(); got != "Xhello" {
		t.Fatalf("body edit = %q", got)
	}
	model.pendingJobID = "already-queued-mail"
	model.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if got := composer.GetBody(); got != "hello" {
		t.Fatalf("Vim undo failed: %q", got)
	}
	if model.pendingJobID != "already-queued-mail" {
		t.Fatal("Vim undo also cancelled a queued email")
	}
}

func TestComposerVimBodyKeepsPaletteShortcutForEditing(t *testing.T) {
	model, composer := appWithVimComposer(t)
	model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if model.palette.IsOpen() {
		t.Fatal("command palette intercepted Vim body editing")
	}
	model.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	if composer.GetBody() == "hello" {
		t.Fatal("text input was lost after the palette shortcut")
	}
}
