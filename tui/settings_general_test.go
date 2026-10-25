package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/config"
)

func TestGeneralComposerVimModePersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config.ClearSessionKey()
	t.Cleanup(config.ClearSessionKey)
	cfg := &config.Config{}
	settings := NewSettings(cfg)
	settings.activePane = PaneContent
	settings.activeCategory = CategoryGeneral
	settings.generalCursor = 16

	for _, want := range []bool{true, false} {
		settings.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cfg.ComposerVimMode != want {
			t.Fatalf("composer Vim mode = %v, want %v", cfg.ComposerVimMode, want)
		}
		loaded, err := config.LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if loaded.ComposerVimMode != want {
			t.Fatalf("saved composer Vim mode = %v, want %v", loaded.ComposerVimMode, want)
		}
	}
}

func TestGeneralComposerVimModeSaveFailure(t *testing.T) {
	for _, initial := range []bool{false, true} {
		name := "disabled"
		if initial {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			config.ClearSessionKey()
			t.Cleanup(config.ClearSessionKey)
			// A file in place of the config directory makes saving fail even as root.
			if err := os.WriteFile(filepath.Join(home, ".config"), []byte("not a directory"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{ComposerVimMode: initial}
			settings := NewSettings(cfg)
			settings.handleGeneralOption(16, keyEnter)
			if cfg.ComposerVimMode != initial {
				t.Fatalf("failed save changed preference to %v, want %v", cfg.ComposerVimMode, initial)
			}
		})
	}
}
