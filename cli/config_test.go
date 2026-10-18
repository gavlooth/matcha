package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/floatpane/matcha/config"
	"github.com/zalando/go-keyring"
)

func TestEditEncryptedConfig(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	if err := config.EnableSecureMode("test-password", &config.Config{ExternalEditor: "my-editor --wait", Theme: "Matcha"}); err != nil {
		t.Fatalf("EnableSecureMode() failed: %v", err)
	}
	t.Cleanup(config.ClearSessionKey)
	configPath := filepath.Join(os.Getenv("HOME"), ".config", "matcha", "config.json")
	encryptedBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	type editorCall struct{ command, path string }
	writeEach := func(contents ...string) (func(string, string) error, *[]editorCall) {
		var editorCalls []editorCall
		return func(editorCommand, path string) error {
			editorCalls = append(editorCalls, editorCall{editorCommand, path})
			return os.WriteFile(path, []byte(contents[len(editorCalls)-1]), 0o600)
		}, &editorCalls
	}

	t.Run("invalid JSON without retry leaves the file untouched", func(t *testing.T) {
		openEditor, calls := writeEach(`{"theme": `)
		err := editEncryptedConfig(configPath, openEditor, func(error) bool { return false })
		if err == nil {
			t.Fatal("invalid JSON should be reported")
		}
		if after, _ := os.ReadFile(configPath); string(after) != string(encryptedBefore) {
			t.Fatal("invalid JSON changed config.json")
		}
		tempDir := filepath.Dir((*calls)[0].path)
		if _, err := os.Stat(tempDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("decrypted copy was left in %s", tempDir)
		}
	})

	t.Run("retry after invalid JSON saves the corrected file encrypted", func(t *testing.T) {
		openEditor, calls := writeEach(`{"theme": `, `{"accounts": [], "theme": "Dark", "external_editor": "my-editor --wait"}`)
		retries := 0
		err := editEncryptedConfig(configPath, openEditor, func(error) bool { retries++; return true })
		if err != nil {
			t.Fatalf("editEncryptedConfig() failed: %v", err)
		}
		if retries != 1 || len(*calls) != 2 {
			t.Fatalf("got %d retries and editor calls %v, want one retry and two calls", retries, *calls)
		}
		if (*calls)[0].command != "my-editor --wait" {
			t.Fatalf("editor command from the encrypted config was not used: %q", (*calls)[0].command)
		}
		onDisk, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if json.Valid(onDisk) {
			t.Fatal("config.json was written back unencrypted")
		}
		decrypted, err := config.SecureReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			Theme string `json:"theme"`
		}
		if err := json.Unmarshal(decrypted, &saved); err != nil || saved.Theme != "Dark" {
			t.Fatalf("saved config = %s (error %v), want theme Dark", decrypted, err)
		}
	})
}
