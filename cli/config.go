package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/floatpane/matcha/config"
)

// RunConfig handles `matcha config [plugin_name]`.
func RunConfig(args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot find home directory: %w", err)
	}

	configPath := filepath.Join(home, ".config", "matcha", "config.json")
	var target string
	if len(args) == 0 {
		target = configPath
	} else {
		name := args[0]
		// Add .lua extension if not present
		if filepath.Ext(name) != ".lua" {
			name += ".lua"
		}
		target = filepath.Join(home, ".config", "matcha", "plugins", name)
	}

	if _, err := os.Stat(target); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", target)
	}

	if target == configPath && config.IsSecureModeEnabled() {
		password, err := promptForPassword()
		if err != nil {
			return fmt.Errorf("password prompt failed: %w", err)
		}
		if _, err := config.VerifyPassword(password); err != nil {
			return fmt.Errorf("incorrect password")
		}
		defer config.ClearSessionKey()
		return editEncryptedConfig(configPath, runEditor, askToEditAgain)
	}

	// Encrypted or broken config files simply yield no external_editor here.
	configJSON, _ := os.ReadFile(configPath)
	return runEditor(editorCommandFromConfigJSON(configJSON), target)
}

// editEncryptedConfig decrypts config.json into a private temporary file, opens
// it in the editor, and encrypts the result back into place. The vault must be
// unlocked. Unchanged files are not rewritten and invalid JSON is never saved.
func editEncryptedConfig(
	configPath string,
	openEditor func(editorCommand, path string) error,
	editAgain func(invalid error) bool,
) error {
	original, err := config.SecureReadFile(configPath)
	if err != nil {
		return fmt.Errorf("cannot decrypt %s: %w", configPath, err)
	}

	// XDG_RUNTIME_DIR is a per-user tmpfs on most Linux systems, so the
	// decrypted copy stays in memory there. When it is unset or unusable the
	// system temp directory is used.
	dir, err := os.MkdirTemp(os.Getenv("XDG_RUNTIME_DIR"), "matcha-config-")
	if err != nil {
		dir, err = os.MkdirTemp("", "matcha-config-")
	}
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	plainPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(plainPath, original, 0o600); err != nil {
		return err
	}

	for {
		if err := openEditor(editorCommandFromConfigJSON(original), plainPath); err != nil {
			return err
		}
		edited, err := os.ReadFile(plainPath)
		if err != nil {
			return err
		}
		if bytes.Equal(edited, original) {
			return nil
		}
		var parsed any
		if err := json.Unmarshal(edited, &parsed); err != nil {
			invalid := fmt.Errorf("config.json is not valid JSON (%w); nothing was saved", err)
			if editAgain(invalid) {
				continue
			}
			return invalid
		}
		return config.SecureWriteFile(configPath, edited, 0o600)
	}
}

func askToEditAgain(invalid error) bool {
	fmt.Fprintln(os.Stderr, invalid)
	fmt.Fprint(os.Stderr, "Edit again? [Y/n] ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

func runEditor(editorCommand, path string) error {
	parts := strings.Fields(editorCommand)
	cmd := exec.Command(parts[0], append(parts[1:], path)...) //nolint:noctx
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// editorCommandFromConfigJSON reads only external_editor so that a config file
// that fails full validation can still be opened and fixed.
func editorCommandFromConfigJSON(configJSON []byte) string {
	var settings struct {
		ExternalEditor string `json:"external_editor"`
	}
	_ = json.Unmarshal(configJSON, &settings)
	return (&config.Config{ExternalEditor: settings.ExternalEditor}).ExternalEditorCommand()
}
