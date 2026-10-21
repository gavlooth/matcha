package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMailRulesRemainRecoverableAfterEncryptionMigrationFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix file permissions enforced for a non-root user")
	}
	home := isolatedMailRulesHome(t)
	rule := validTestMailRule()
	if err := SaveMailRules([]MailRule{rule}); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(home, ".cache", "matcha")
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cache, "email_cache.json")
	if err := os.WriteFile(path, []byte(`{}`), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cache, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cache, 0700) })
	if err := EnableSecureMode("recovery-test-password", &Config{}); err == nil {
		t.Fatal("encryption migration succeeded despite unwritable cache")
	}
	ClearSessionKey()
	if _, err := VerifyPassword("recovery-test-password"); err != nil {
		t.Fatalf("cannot unlock partly encrypted files after migration failure: %v", err)
	}
	rules, err := LoadMailRules()
	if err != nil || len(rules) != 1 || rules[0].From != rule.From || rules[0].Name != rule.Name {
		t.Fatalf("rules were lost after migration failure: %v, %v", rules, err)
	}
}
