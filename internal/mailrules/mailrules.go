// Package mailrules runs local IMAP INBOX rules without depending on the app or UI.
package mailrules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
)

const progressFileName = "mail_rules_progress.json"

// Result includes completed server effects even when another action or saving
// progress fails. More requests a near-term run for another successful batch.
type Result struct {
	Changed bool
	More    bool
	Errors  []error
}

// All runners serialize read/apply/write cycles in this process and hold an OS
// advisory lock across processes. A restart always reloads disk state.
var runMu sync.Mutex

// Runner is concurrency-safe. Use NewRunner or the zero value.
type Runner struct {
	apply func(*config.Account, []config.MailRule, fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error)
}

func NewRunner() *Runner {
	return &Runner{apply: fetcher.ApplyMailRules}
}

type accountProgress struct {
	Definitions string                   `json:"definitions"`
	Cursor      fetcher.MailRuleProgress `json:"cursor"`
}

type progressFile struct {
	Version  int                        `json:"version"`
	Accounts map[string]accountProgress `json:"accounts"`
}

// Run handles at most 100 current INBOX messages per applicable account. Only
// enabled rules belonging to that account participate, in their saved order.
// Missing rules and accounts without applicable rules do not touch progress or
// open connections. Definition or delivery-scope changes baseline future mail.
func (runner *Runner) Run(accounts []config.Account, rules []config.MailRule) Result {
	if !runMu.TryLock() {
		return Result{}
	}
	defer runMu.Unlock()
	var result Result
	if err := config.ValidateMailRules(rules); err != nil {
		result.Errors = append(result.Errors, err)
		return result
	}
	type work struct {
		account config.Account
		rules   []config.MailRule
	}
	var pending []work
	seen := make(map[string]bool)
	for _, account := range accounts {
		var applicable []config.MailRule
		for _, rule := range rules {
			if rule.Enabled && (rule.AccountID == "" || rule.AccountID == account.ID) {
				applicable = append(applicable, rule)
			}
		}
		if len(applicable) == 0 {
			continue
		}
		if account.ID == "" || seen[account.ID] {
			result.Errors = append(result.Errors, errors.New("local mail rules require unique, nonempty account IDs"))
			continue
		}
		seen[account.ID] = true
		pending = append(pending, work{account: account, rules: applicable})
	}
	if len(pending) == 0 {
		return result
	}
	dir, err := config.GetCacheDir()
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("locate mail rule progress: %w", err))
		return result
	}
	lockDir, err := config.GetConfigDir()
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("locate mail rule lock: %w", err))
		return result
	}
	lock, acquired, err := lockProgress(lockDir)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("lock mail rule progress: %w", err))
		return result
	}
	if !acquired {
		return result
	}
	defer lock.Close() //nolint:errcheck
	path := filepath.Join(dir, progressFileName)
	saved, err := loadProgress(path)
	if err != nil {
		// Do not replay old mail on an unreadable/corrupt checkpoint.
		result.Errors = append(result.Errors, err)
		return result
	}
	apply := runner.apply
	if apply == nil {
		apply = fetcher.ApplyMailRules
	}
	for _, item := range pending {
		fingerprint := definitionsFingerprint(item.account, item.rules)
		previous := saved.Accounts[item.account.ID]
		cursor := previous.Cursor
		if previous.Definitions != fingerprint {
			cursor = fetcher.MailRuleProgress{}
		}
		batch, err := apply(&item.account, item.rules, cursor)
		result.Changed = result.Changed || batch.Changed
		if err != nil {
			result.Errors = append(result.Errors, err)
		} else {
			result.More = result.More || batch.More
		}
		next := accountProgress{Definitions: fingerprint, Cursor: batch.Progress}
		if next == previous || batch.Progress.UIDValidity == 0 {
			continue
		}
		saved.Accounts[item.account.ID] = next
		// Save each account's safe prefix even on partial action failure. Never
		// checkpoint the failing UID, including mark-read success + move failure.
		data, marshalErr := json.Marshal(saved)
		if marshalErr == nil {
			marshalErr = config.AtomicSecureWriteFile(path, data)
		}
		if marshalErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("account %q: save mail rule progress: %w", item.account.ID, marshalErr))
			result.More = false
			// Stop: more actions without durable checkpoints compound replay.
			return result
		}
	}
	return result
}

func loadProgress(path string) (progressFile, error) {
	saved := progressFile{Version: 1, Accounts: make(map[string]accountProgress)}
	data, err := config.SecureReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return saved, nil
	}
	if err != nil {
		return saved, fmt.Errorf("read mail rule progress: %w", err)
	}
	if err := json.Unmarshal(data, &saved); err != nil || saved.Version != 1 || saved.Accounts == nil {
		return saved, errors.New("mail rule progress is invalid or unsupported; no mail was processed")
	}
	for _, checkpoint := range saved.Accounts {
		if len(checkpoint.Definitions) != sha256.Size*2 || checkpoint.Cursor.UIDValidity == 0 {
			return saved, errors.New("mail rule progress contains an invalid checkpoint; no mail was processed")
		}
	}
	return saved, nil
}

func definitionsFingerprint(account config.Account, rules []config.MailRule) string {
	// Names and disabled/unrelated rules do not affect matching behavior. The
	// ordered active definitions and delivery identity do; credentials do not.
	type activeRule struct {
		Definition config.MailRule
		Activation string
	}
	canonical := make([]activeRule, len(rules))
	for i, rule := range rules {
		rule.Name = ""
		rule.AccountID = ""
		rule.Enabled = false
		canonical[i] = activeRule{Definition: rule, Activation: rule.ActivationID()}
	}
	identity := struct {
		ID       string
		Server   string
		Port     int
		Email    string
		Fetch    string
		Provider string
		Protocol string
		CatchAll bool
		Rules    []activeRule
	}{account.ID, account.GetIMAPServer(), account.GetIMAPPort(), account.Email, account.FetchEmail, account.ServiceProvider, account.Protocol, account.CatchAll, canonical}
	data, _ := json.Marshal(identity) // contains only strings, bools, ints and rules
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
