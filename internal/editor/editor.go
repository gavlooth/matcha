package editor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/tui"
)

// OpenExternalEditor writes the body to a temp file, opens editorCommand on it,
// and reads back the result. editorCommand may include arguments.
func OpenExternalEditor(editorCommand, body string) tea.Cmd {
	parts := strings.Fields(editorCommand)
	if len(parts) == 0 {
		parts = []string{"vi"}
	}

	tmpFile, err := os.CreateTemp("", "matcha-*.md")
	if err != nil {
		return func() tea.Msg {
			return tui.EditorFinishedMsg{Err: fmt.Errorf("creating temp file: %w", err)}
		}
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(body); err != nil {
		writeErr := err
		if err := tmpFile.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return func() tea.Msg {
				return tui.EditorFinishedMsg{Err: fmt.Errorf("closing temp file after write failure: %w", err)}
			}
		}
		_ = os.Remove(tmpPath)
		return func() tea.Msg {
			return tui.EditorFinishedMsg{Err: fmt.Errorf("writing temp file: %w", writeErr)}
		}
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return func() tea.Msg {
			return tui.EditorFinishedMsg{Err: fmt.Errorf("closing temp file: %w", err)}
		}
	}

	args := append(parts[1:], tmpPath)   //nolint:gocritic
	c := exec.Command(parts[0], args...) //nolint:noctx
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer func() {
			_ = os.Remove(tmpPath)
		}()
		if err != nil {
			return tui.EditorFinishedMsg{Err: err}
		}
		content, readErr := os.ReadFile(tmpPath)
		if readErr != nil {
			return tui.EditorFinishedMsg{Err: readErr}
		}
		return tui.EditorFinishedMsg{Body: string(content)}
	})
}
