package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// MailRule is a local, ordered INBOX rule. Empty AccountID applies to all accounts.
// Every nonempty condition must match; the first enabled match wins.
type MailRule struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	AccountID  string `json:"account_id,omitempty"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	Subject    string `json:"subject,omitempty"`
	MoveTo     string `json:"move_to,omitempty"`
	MarkRead   bool   `json:"mark_read,omitempty"`
	activation string
}

// ActivationID identifies a saved rule's activation, including disable/enable
// cycles with otherwise identical definitions. It is not user-editable state.
func (rule MailRule) ActivationID() string {
	return rule.activation
}

type storedMailRule struct {
	MailRule
	Activation string `json:"_activation"`
}

const mailRulesFileName = "mail_rules.json"

var mailRuleIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// ValidateMailRule validates a definition without changing it or accessing mail.
func ValidateMailRule(rule MailRule) error {
	if !mailRuleIDPattern.MatchString(rule.ID) {
		return errors.New("rule ID must be 1-128 letters, digits, hyphens or underscores, starting with a letter or digit")
	}
	if strings.TrimSpace(rule.Name) == "" {
		return errors.New("rule name is required")
	}
	if strings.TrimSpace(rule.From) == "" && strings.TrimSpace(rule.To) == "" && strings.TrimSpace(rule.Subject) == "" {
		return errors.New("at least one From, To or Subject condition is required")
	}
	if rule.MoveTo == "" && !rule.MarkRead {
		return errors.New("at least one move or mark-read action is required")
	}
	if rule.MoveTo != "" {
		if strings.TrimSpace(rule.MoveTo) == "" || strings.ContainsAny(rule.MoveTo, "\x00\r\n") {
			return errors.New("move destination must be a valid mailbox name")
		}
		if strings.EqualFold(strings.TrimSpace(rule.MoveTo), "INBOX") {
			return errors.New("move destination cannot be INBOX")
		}
	}
	return nil
}

// ValidateMailRules also rejects duplicate IDs while preserving rule order.
func ValidateMailRules(rules []MailRule) error {
	seen := make(map[string]bool, len(rules))
	for i, rule := range rules {
		if err := ValidateMailRule(rule); err != nil {
			return fmt.Errorf("rule %d: %w", i+1, err)
		}
		if seen[rule.ID] {
			return fmt.Errorf("rule %d: duplicate rule ID", i+1)
		}
		seen[rule.ID] = true
	}
	return nil
}

// LoadMailRules reads only the separate rules file; a missing file means no rules.
func LoadMailRules() ([]MailRule, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	data, err := SecureReadFile(filepath.Join(dir, mailRulesFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read mail rules: %w", err)
	}
	var stored []storedMailRule
	if err := json.Unmarshal(data, &stored); err != nil {
		// Decoder errors can quote private condition text; do not return it.
		return nil, errors.New("mail rules file contains invalid JSON definitions")
	}
	rules := make([]MailRule, len(stored))
	for i, entry := range stored {
		rules[i] = entry.MailRule
		rules[i].activation = entry.Activation
	}
	if err := ValidateMailRules(rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// SaveMailRules atomically saves validated definitions without rewriting config.json.
func SaveMailRules(rules []MailRule) error {
	if err := ValidateMailRules(rules); err != nil {
		return err
	}
	dir, err := configDir()
	if err != nil {
		return err
	}
	previous, err := LoadMailRules()
	if err != nil {
		return err
	}
	byID := make(map[string]MailRule, len(previous))
	for _, rule := range previous {
		byID[rule.ID] = rule
	}
	stored := make([]storedMailRule, len(rules))
	for i, rule := range rules {
		old, exists := byID[rule.ID]
		activation := old.activation
		// A cosmetic rename does not baseline again. Every behavior change,
		// including enable, gets a fresh activation even if old content returns.
		old.Name, rule.Name = "", ""
		old.activation, rule.activation = "", ""
		if !exists || activation == "" || old != rule {
			activation = uuid.NewString()
		}
		stored[i] = storedMailRule{MailRule: rules[i], Activation: activation}
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	return AtomicSecureWriteFile(filepath.Join(dir, mailRulesFileName), data)
}
