package oauthcmd

import (
	"errors"
	"os"
	"os/exec"

	"github.com/floatpane/matcha/cli"
	"github.com/floatpane/matcha/internal/loglevel"
)

// Run implements the CLI entrypoint for `matcha oauth`.
// Usage:
//
//	matcha oauth auth   <email> [--provider gmail|outlook|yahoo] [--client-id ID --client-secret SECRET]
//	matcha oauth token  <email>
//	matcha oauth revoke <email>
func Run(args []string) {
	if len(args) < 1 {
		loglevel.Infof("Usage: matcha oauth <auth|token|revoke> <email> [flags]")
		loglevel.Infof("")
		loglevel.Infof("Commands:")
		loglevel.Infof("  auth   <email>  Sign in through the browser and store tokens for Matcha")
		loglevel.Infof("  token  <email>  Print a fresh access token (refreshes automatically)")
		loglevel.Infof("  revoke <email>  Revoke and delete stored OAuth2 tokens")
		loglevel.Infof("")
		loglevel.Infof("Flags for auth:")
		loglevel.Infof("  --provider gmail|outlook|yahoo  OAuth2 provider (auto-detected from email)")
		loglevel.Infof("  --client-id ID                  Your own OAuth2 app's client ID (optional)")
		loglevel.Infof("  --client-secret SECRET          Your own OAuth2 app's client secret (optional)")
		loglevel.Infof("")
		loglevel.Infof("The app registration comes from --client-id/--client-secret (then remembered in")
		loglevel.Infof("~/.config/matcha/oauth_client.json for Gmail, oauth_client_<provider>.json otherwise),")
		loglevel.Infof("or from the provider's entry in ~/.config/matcha/oauth_apps.json.")
		os.Exit(1)
	}

	script, err := cli.OAuthScriptPath()
	if err != nil {
		loglevel.Infof("Error: %v", err)
		os.Exit(1)
	}

	cmdArgs := append([]string{script}, args...)
	cmd := exec.Command("python3", cmdArgs...) //nolint:noctx
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		loglevel.Infof("Error: %v", err)
		os.Exit(1)
	}
}
