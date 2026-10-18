package config

import (
	"strings"
	"testing"
)

func TestOAuth2AccessTokenFromCommand(t *testing.T) {
	acc := &Account{Email: "a@example.com", AuthMethod: "oauth2", OAuthTokenCmd: "printf '  token-123\\n'"}
	got, err := acc.OAuth2AccessToken()
	if err != nil || got != "token-123" {
		t.Fatalf("OAuth2AccessToken() = %q, %v; want token-123", got, err)
	}

	acc.OAuthTokenCmd = "echo 'saved login missing' >&2; exit 1"
	if _, err := acc.OAuth2AccessToken(); err == nil || !strings.Contains(err.Error(), "saved login missing") {
		t.Fatalf("failing command error = %v, want its error output", err)
	}

	acc.OAuthTokenCmd = "true"
	if _, err := acc.OAuth2AccessToken(); err == nil {
		t.Fatal("empty command output should be an error")
	}
}
