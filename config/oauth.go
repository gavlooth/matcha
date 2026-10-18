package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed oauth_script.py
var embeddedOAuthScript []byte

// IsOAuth2 returns true if the account uses OAuth2 authentication.
func (a *Account) IsOAuth2() bool {
	return a.AuthMethod == "oauth2"
}

// OAuthScriptPath returns the path to the OAuth2 Python helper script.
// The script is embedded in the binary and extracted to ~/.config/matcha/oauth/.
// It is replaced only when its contents differ, and through a rename, so that
// concurrent token requests never run a half-written script.
func OAuthScriptPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}

	scriptDir := filepath.Join(dir, "oauth")
	scriptPath := filepath.Join(scriptDir, "oauth.py")

	if current, err := os.ReadFile(scriptPath); err == nil && bytes.Equal(current, embeddedOAuthScript) {
		return scriptPath, nil
	}
	if err := os.MkdirAll(scriptDir, 0700); err != nil {
		return "", fmt.Errorf("could not create oauth directory: %w", err)
	}
	tmp, err := os.CreateTemp(scriptDir, ".oauth-*.py")
	if err != nil {
		return "", fmt.Errorf("could not extract oauth script: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(embeddedOAuthScript); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("could not extract oauth script: %w", err)
	}
	if err := tmp.Chmod(0700); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("could not extract oauth script: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("could not extract oauth script: %w", err)
	}
	if err := os.Rename(tmp.Name(), scriptPath); err != nil {
		return "", fmt.Errorf("could not extract oauth script: %w", err)
	}
	return scriptPath, nil
}

// OAuth2AccessToken returns a fresh OAuth2 access token for the account. It
// runs the account's oauth_token_cmd when set, and otherwise the built-in
// Python helper, which refreshes stored tokens as needed. The command's error
// output is returned in the error instead of being written to the terminal,
// where it would corrupt the TUI.
func (a *Account) OAuth2AccessToken() (string, error) {
	var cmd *exec.Cmd
	if strings.TrimSpace(a.OAuthTokenCmd) != "" {
		cmd = exec.Command("sh", "-c", a.OAuthTokenCmd) //nolint:noctx
	} else {
		script, err := OAuthScriptPath()
		if err != nil {
			return "", err
		}
		cmd = exec.Command("python3", script, "token", a.Email) //nolint:noctx
	}

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if detail := strings.TrimSpace(string(exitErr.Stderr)); detail != "" {
				return "", fmt.Errorf("oauth2 token retrieval failed for %s: %s", a.Email, detail)
			}
		}
		return "", fmt.Errorf("oauth2 token retrieval failed for %s: %w", a.Email, err)
	}

	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("oauth2: empty access token returned for %s", a.Email)
	}
	return token, nil
}

// OAuth2SignInCommand returns the command that signs the account in through
// the browser and stores Matcha's own tokens for it. It talks to the user on
// the terminal (a redirect address that cannot be served locally is pasted
// back), so run it with the terminal attached, e.g. via tea.Exec. The app
// registration comes from ~/.config/matcha/oauth_apps.json or oauth_client*.json.
func (a *Account) OAuth2SignInCommand() (*exec.Cmd, error) {
	script, err := OAuthScriptPath()
	if err != nil {
		return nil, err
	}
	args := []string{script, "auth", a.Email}
	// Other providers (e.g. "custom" for Yahoo) are detected from the address.
	if a.ServiceProvider == ProviderGmail || a.ServiceProvider == ProviderOutlook {
		args = append(args, "--provider", a.ServiceProvider)
	}
	return exec.Command("python3", args...), nil //nolint:noctx
}
