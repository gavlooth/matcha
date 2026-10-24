package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func isolatedMailRulesHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	reset := func() {
		vaultMu.Lock()
		defer vaultMu.Unlock()
		if cachedVault != nil {
			cachedVault.Lock()
		}
		cachedVault = nil
	}
	reset()
	t.Cleanup(reset)
	return home
}

func validTestMailRule() MailRule {
	return MailRule{ID: "rule-1", Name: "Newsletters", Enabled: true, From: "news@example.com", MarkRead: true}
}

func TestValidateMailRules(t *testing.T) {
	valid := validTestMailRule()
	cases := []struct {
		name   string
		change func(*MailRule)
	}{
		{"missing ID", func(r *MailRule) { r.ID = "" }},
		{"path ID", func(r *MailRule) { r.ID = "../rule" }},
		{"control ID", func(r *MailRule) { r.ID = "rule\n1" }},
		{"blank name", func(r *MailRule) { r.Name = " \t" }},
		{"no conditions", func(r *MailRule) { r.From = "" }},
		{"blank condition", func(r *MailRule) { r.From = " " }},
		{"no actions", func(r *MailRule) { r.MarkRead = false }},
		{"INBOX move", func(r *MailRule) { r.MoveTo = " inBoX " }},
		{"blank mailbox", func(r *MailRule) { r.MoveTo = " " }},
		{"mailbox control", func(r *MailRule) { r.MoveTo = "News\r\nINBOX" }},
	}
	if err := ValidateMailRule(valid); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := valid
			tc.change(&rule)
			if err := ValidateMailRule(rule); err == nil {
				t.Fatal("invalid definition was accepted")
			}
		})
	}
	if err := ValidateMailRules([]MailRule{valid, valid}); err == nil {
		t.Fatal("duplicate IDs were accepted")
	}
	moveOnly := valid
	moveOnly.MarkRead, moveOnly.MoveTo = false, "News/Updates"
	if err := ValidateMailRule(moveOnly); err != nil {
		t.Fatal(err)
	}

	showOnly := valid
	showOnly.MarkRead, showOnly.ShowInAccount = false, true
	if err := ValidateMailRule(showOnly); err != nil {
		t.Fatalf("show-only rule should be valid: %v", err)
	}
}

func TestMailRulesSaveLoadPreservesUserConfiguration(t *testing.T) {
	home := isolatedMailRulesHome(t)
	if rules, err := LoadMailRules(); err != nil || len(rules) != 0 {
		t.Fatalf("missing file: rules=%v err=%v", rules, err)
	}
	dir := filepath.Join(home, ".config", "matcha")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")
	original := []byte("{\"unknown_future_setting\":true}\n")
	if err := os.WriteFile(configPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	rule := validTestMailRule()
	second := rule
	second.ID, second.Name, second.AccountID = "rule-2", "Scoped", "account-a"
	want := []MailRule{second, rule}
	if err := SaveMailRules(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMailRules()
	for i := range got {
		got[i].activation = ""
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("definitions or order changed: got=%+v err=%v", got, err)
	}
	if data, err := os.ReadFile(configPath); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("saving rules rewrote user configuration: %v", err)
	}
	path := filepath.Join(dir, mailRulesFileName)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("rule file must be private: info=%v err=%v", info, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveMailRules([]MailRule{rule, rule}); err == nil {
		t.Fatal("duplicate definitions were saved")
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rejected save changed existing definitions: %v", err)
	}
	if err := SaveMailRules(nil); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "[]" {
		t.Fatalf("empty definitions should be a JSON list: %q %v", data, err)
	}
	if err := os.WriteFile(path, []byte(`[{"id":"broken"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMailRules(); err == nil {
		t.Fatal("invalid on-disk rule was accepted")
	}
}

func TestMailRulesAndProgressParticipateInEncryptionMigration(t *testing.T) {
	home := isolatedMailRulesHome(t)
	rule := validTestMailRule()
	if err := SaveMailRules([]MailRule{rule}); err != nil {
		t.Fatal(err)
	}
	progressPath := filepath.Join(home, ".cache", "matcha", "mail_rules_progress.json")
	progress := []byte(`{"version":1,"accounts":{}}`)
	if err := AtomicSecureWriteFile(progressPath, progress); err != nil {
		t.Fatal(err)
	}
	rulesPath := filepath.Join(home, ".config", "matcha", mailRulesFileName)
	files, err := collectDataFiles()
	if err != nil || !slices.Contains(files, rulesPath) || !slices.Contains(files, progressPath) {
		t.Fatalf("encryption migration omitted rules or progress: files=%v err=%v", files, err)
	}
	if err := EnableSecureMode("isolated-test-passphrase", nil); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{rulesPath, progressPath} {
		data, err := os.ReadFile(path)
		if err != nil || json.Valid(data) {
			t.Fatalf("file was not encrypted: %s err=%v", filepath.Base(path), err)
		}
	}
	if rules, err := LoadMailRules(); err != nil || len(rules) != 1 || !samePublicMailRule(rules[0], rule) {
		t.Fatalf("encrypted rules did not round-trip: %v %v", rules, err)
	}
	rule.Subject = "private subject"
	if err := SaveMailRules([]MailRule{rule}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(rulesPath); err != nil || json.Valid(data) || bytes.Contains(data, []byte(rule.Subject)) {
		t.Fatalf("atomic save bypassed encryption: %v", err)
	}
	if err := DisableSecureMode(nil); err != nil {
		t.Fatal(err)
	}
	if rules, err := LoadMailRules(); err != nil || len(rules) != 1 || !samePublicMailRule(rules[0], rule) {
		t.Fatalf("disabling encryption lost definitions: %v %v", rules, err)
	}
	if data, err := os.ReadFile(progressPath); err != nil || !bytes.Equal(data, progress) {
		t.Fatalf("disabling encryption lost progress: %q %v", data, err)
	}
}

func samePublicMailRule(a, b MailRule) bool {
	a.activation, b.activation = "", ""
	return a == b
}

func TestMailRuleActivationChangesOnlyForBehaviorEdits(t *testing.T) {
	isolatedMailRulesHome(t)
	rule := validTestMailRule()
	saveAndLoad := func() MailRule {
		t.Helper()
		if err := SaveMailRules([]MailRule{rule}); err != nil {
			t.Fatal(err)
		}
		rules, err := LoadMailRules()
		if err != nil || len(rules) != 1 {
			t.Fatalf("load: rules=%v err=%v", rules, err)
		}
		return rules[0]
	}
	first := saveAndLoad()
	if first.ActivationID() == "" {
		t.Fatal("new definition has no activation revision")
	}
	rule.Name = "Cosmetic rename"
	renamed := saveAndLoad()
	if renamed.ActivationID() != first.ActivationID() {
		t.Fatal("cosmetic rename reset activation")
	}
	rule.Enabled = false
	disabled := saveAndLoad()
	rule.Enabled = true
	enabled := saveAndLoad()
	if enabled.ActivationID() == first.ActivationID() || enabled.ActivationID() == disabled.ActivationID() {
		t.Fatal("disable/enable reused the old activation")
	}
	rule.Subject = "new matching condition"
	edited := saveAndLoad()
	if edited.ActivationID() == enabled.ActivationID() {
		t.Fatal("condition edit did not reset activation")
	}
}
