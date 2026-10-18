package cli

import (
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

	editorParts := strings.Fields(editorCommandFromConfigFile(configPath))
	cmd := exec.Command(editorParts[0], append(editorParts[1:], target)...) //nolint:noctx
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// editorCommandFromConfigFile reads only external_editor so that a config file
// that fails full validation can still be opened and fixed.
func editorCommandFromConfigFile(configPath string) string {
	var settings struct {
		ExternalEditor string `json:"external_editor"`
	}
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	return (&config.Config{ExternalEditor: settings.ExternalEditor}).ExternalEditorCommand()
}
