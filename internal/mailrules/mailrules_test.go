package mailrules

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
)

func ruleRunnerHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func runnerRule() config.MailRule {
	return config.MailRule{ID: "rule", Name: "News", Enabled: true, Subject: "news", MarkRead: true}
}

func TestRunnerPersistsSafeProgressAcrossRestartAndPartialFailure(t *testing.T) {
	home := ruleRunnerHome(t)
	accounts := []config.Account{{ID: "a", Email: "me@example.com"}}
	rules := []config.MailRule{runnerRule()}
	first := NewRunner()
	first.apply = func(_ *config.Account, _ []config.MailRule, cursor fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		if cursor != (fetcher.MailRuleProgress{}) {
			t.Fatalf("first activation did not start empty: %+v", cursor)
		}
		return fetcher.MailRuleBatch{Progress: fetcher.MailRuleProgress{UIDValidity: 7, LastUID: 40}}, nil
	}
	if result := first.Run(accounts, rules); result.Changed || result.More || len(result.Errors) != 0 {
		t.Fatalf("baseline: %+v", result)
	}
	path := filepath.Join(home, ".cache", "matcha", progressFileName)
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("checkpoint must be 0600: %v %v", info, err)
	}
	restarted := NewRunner()
	restarted.apply = func(_ *config.Account, _ []config.MailRule, cursor fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		if cursor != (fetcher.MailRuleProgress{UIDValidity: 7, LastUID: 40}) {
			t.Fatalf("restart lost baseline checkpoint: %+v", cursor)
		}
		return fetcher.MailRuleBatch{Progress: fetcher.MailRuleProgress{UIDValidity: 7, LastUID: 42}, Changed: true, More: true}, errors.New("rule failed at UID 43")
	}
	partial := restarted.Run(accounts, rules)
	if !partial.Changed || partial.More || len(partial.Errors) != 1 {
		t.Fatalf("partial effects or error were lost: %+v", partial)
	}
	next := NewRunner()
	next.apply = func(_ *config.Account, _ []config.MailRule, cursor fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		if cursor != (fetcher.MailRuleProgress{UIDValidity: 7, LastUID: 42}) {
			t.Fatalf("safe prefix was not persisted after failure: %+v", cursor)
		}
		return fetcher.MailRuleBatch{Progress: fetcher.MailRuleProgress{UIDValidity: 7, LastUID: 142}, Changed: true, More: true}, nil
	}
	if result := next.Run(accounts, rules); !result.Changed || !result.More || len(result.Errors) != 0 {
		t.Fatalf("resumed batch: %+v", result)
	}
	again := NewRunner()
	again.apply = func(_ *config.Account, _ []config.MailRule, cursor fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		if cursor.LastUID != 142 || cursor.UIDValidity != 7 {
			t.Fatalf("batch continuation was not persisted: %+v", cursor)
		}
		return fetcher.MailRuleBatch{Progress: cursor}, nil
	}
	if result := again.Run(accounts, rules); len(result.Errors) != 0 {
		t.Fatal(result.Errors)
	}
}

func TestRunnerDefinitionAndUIDValidityResetAreDurable(t *testing.T) {
	ruleRunnerHome(t)
	accounts := []config.Account{{ID: "a", Email: "me@example.com"}}
	rule := runnerRule()
	runner := NewRunner()
	calls := 0
	runner.apply = func(_ *config.Account, _ []config.MailRule, cursor fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		calls++
		switch calls {
		case 1:
			return fetcher.MailRuleBatch{Progress: fetcher.MailRuleProgress{UIDValidity: 1, LastUID: 10}}, nil
		case 2:
			if cursor.LastUID != 10 || cursor.UIDValidity != 1 {
				t.Fatalf("rename reset the active definitions: %+v", cursor)
			}
			// Protocol layer observed a recreated INBOX and baselined UIDNEXT-1.
			return fetcher.MailRuleBatch{Progress: fetcher.MailRuleProgress{UIDValidity: 2, LastUID: 20}}, nil
		case 3:
			if cursor.LastUID != 20 || cursor.UIDValidity != 2 {
				t.Fatalf("UID reset was not saved: %+v", cursor)
			}
			return fetcher.MailRuleBatch{Progress: cursor}, nil
		case 4:
			if cursor != (fetcher.MailRuleProgress{}) {
				t.Fatalf("behavior edit reused the old cursor: %+v", cursor)
			}
			return fetcher.MailRuleBatch{Progress: fetcher.MailRuleProgress{UIDValidity: 2, LastUID: 30}}, nil
		default:
			t.Fatal("unexpected application")
			return fetcher.MailRuleBatch{}, nil
		}
	}
	for _, mutate := range []func(){func() {}, func() { rule.Name = "renamed" }, func() {}, func() { rule.Subject = "edited" }} {
		mutate()
		if result := runner.Run(accounts, []config.MailRule{rule}); len(result.Errors) != 0 || result.Changed {
			t.Fatalf("activation/reset must not change historical mail: %+v", result)
		}
	}
}

func TestRunnerDisableEnableBaselinesMailReceivedWhileDisabled(t *testing.T) {
	home := ruleRunnerHome(t)
	accounts := []config.Account{{ID: "a", Email: "me@example.com"}}
	rule := runnerRule()
	save := func() []config.MailRule {
		t.Helper()
		if err := config.SaveMailRules([]config.MailRule{rule}); err != nil {
			t.Fatal(err)
		}
		loaded, err := config.LoadMailRules()
		if err != nil {
			t.Fatal(err)
		}
		return loaded
	}
	// This deterministic mailbox snapshot models the protocol layer's already
	// separately tested UIDNEXT baseline. Only arrivals after activation act.
	uidNext := uint32(11)
	var acted []uint32
	apply := func(_ *config.Account, _ []config.MailRule, cursor fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		if cursor.UIDValidity == 0 {
			return fetcher.MailRuleBatch{Progress: fetcher.MailRuleProgress{UIDValidity: 1, LastUID: uidNext - 1}}, nil
		}
		changed := cursor.LastUID < uidNext-1
		for uid := cursor.LastUID + 1; uid < uidNext; uid++ {
			acted = append(acted, uid)
		}
		return fetcher.MailRuleBatch{Progress: fetcher.MailRuleProgress{UIDValidity: 1, LastUID: uidNext - 1}, Changed: changed}, nil
	}
	run := func(rules []config.MailRule) Result {
		runner := NewRunner() // reread progress just as a fresh TUI process would
		runner.apply = apply
		result := runner.Run(accounts, rules)
		if len(result.Errors) != 0 {
			t.Fatal(result.Errors)
		}
		return result
	}
	active := save()
	if result := run(active); result.Changed {
		t.Fatal("initial activation changed existing mail")
	}
	uidNext = 12
	if result := run(active); !result.Changed || !reflect.DeepEqual(acted, []uint32{11}) {
		t.Fatalf("future arrival was not processed: %+v %v", result, acted)
	}
	rule.Enabled = false
	disabled := save()
	progressPath := filepath.Join(home, ".cache", "matcha", progressFileName)
	before, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatal(err)
	}
	uidNext = 15 // UIDs 12-14 arrive while disabled
	if result := run(disabled); result.Changed || result.More {
		t.Fatalf("disabled rule did work: %+v", result)
	}
	if after, err := os.ReadFile(progressPath); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("no enabled rules changed progress: %v", err)
	}
	rule.Enabled = true
	reenabled := save()
	if result := run(reenabled); result.Changed || !reflect.DeepEqual(acted, []uint32{11}) {
		t.Fatalf("re-enable changed mail received while disabled: %+v %v", result, acted)
	}
	uidNext = 16
	if result := run(reenabled); !result.Changed || !reflect.DeepEqual(acted, []uint32{11, 15}) {
		t.Fatalf("future mail after re-enable was not processed: %+v %v", result, acted)
	}
}

func TestRunnerKeepsAccountFingerprintsIndependent(t *testing.T) {
	ruleRunnerHome(t)
	accounts := []config.Account{{ID: "a"}, {ID: "b"}}
	a, b := runnerRule(), runnerRule()
	a.ID, a.AccountID = "a-rule", "a"
	b.ID, b.AccountID = "b-rule", "b"
	runner := NewRunner()
	seen := make(map[string]fetcher.MailRuleProgress)
	runner.apply = func(account *config.Account, rules []config.MailRule, cursor fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		if len(rules) != 1 || rules[0].AccountID != account.ID {
			t.Fatalf("wrong account rules: account=%s rules=%v", account.ID, rules)
		}
		seen[account.ID] = cursor
		return fetcher.MailRuleBatch{Progress: fetcher.MailRuleProgress{UIDValidity: 1, LastUID: 10}}, nil
	}
	if result := runner.Run(accounts, []config.MailRule{a, b}); len(result.Errors) != 0 {
		t.Fatal(result.Errors)
	}
	a.Subject = "changed only a"
	if result := runner.Run(accounts, []config.MailRule{a, b}); len(result.Errors) != 0 {
		t.Fatal(result.Errors)
	}
	if seen["a"] != (fetcher.MailRuleProgress{}) || seen["b"].LastUID != 10 {
		t.Fatalf("account A edit discarded B's arrivals: %v", seen)
	}
}

func TestRunnerNoApplicableRulesDoesNotCreateStateOrApply(t *testing.T) {
	home := ruleRunnerHome(t)
	runner := NewRunner()
	runner.apply = func(*config.Account, []config.MailRule, fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		t.Fatal("no applicable rules must not connect")
		return fetcher.MailRuleBatch{}, nil
	}
	rule := runnerRule()
	rule.AccountID = "other"
	for _, rules := range [][]config.MailRule{nil, {rule}} {
		if result := runner.Run([]config.Account{{ID: "a"}}, rules); result.Changed || result.More || len(result.Errors) != 0 {
			t.Fatalf("no-op result: %+v", result)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no applicable rules touched cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no applicable rules touched lock/config: %v", err)
	}
}

func TestRunnerRejectsCorruptProgressBeforeActions(t *testing.T) {
	home := ruleRunnerHome(t)
	path := filepath.Join(home, ".cache", "matcha", progressFileName)
	if err := config.AtomicSecureWriteFile(path, []byte(`{"version":99,"accounts":{}}`)); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner()
	runner.apply = func(*config.Account, []config.MailRule, fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		t.Fatal("corrupt checkpoint must not trigger historical replay")
		return fetcher.MailRuleBatch{}, nil
	}
	if result := runner.Run([]config.Account{{ID: "a"}}, []config.MailRule{runnerRule()}); len(result.Errors) != 1 || result.Changed {
		t.Fatalf("corrupt checkpoint was ignored: %+v", result)
	}
}

func TestRunnerPreservesChangedWhenCheckpointSaveFails(t *testing.T) {
	home := ruleRunnerHome(t)
	runner := NewRunner()
	runner.apply = func(*config.Account, []config.MailRule, fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		// Replace the destination with a directory only after the state read,
		// causing atomic rename to fail without relying on Unix permissions.
		path := filepath.Join(home, ".cache", "matcha", progressFileName)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		return fetcher.MailRuleBatch{Progress: fetcher.MailRuleProgress{UIDValidity: 1, LastUID: 1}, Changed: true, More: true}, nil
	}
	if result := runner.Run([]config.Account{{ID: "a"}}, []config.MailRule{runnerRule()}); !result.Changed || result.More || len(result.Errors) != 1 {
		t.Fatalf("save failure lost observed server effect: %+v", result)
	}
}

func TestRunnerConcurrentCallsSkipBusyInvocation(t *testing.T) {
	ruleRunnerHome(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	runner := NewRunner()
	runner.apply = func(*config.Account, []config.MailRule, fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		once.Do(func() { close(entered) })
		<-release
		return fetcher.MailRuleBatch{Progress: fetcher.MailRuleProgress{UIDValidity: 1}}, nil
	}
	accounts, rules := []config.Account{{ID: "a"}}, []config.MailRule{runnerRule()}
	done := make(chan Result, 1)
	go func() { done <- runner.Run(accounts, rules) }()
	<-entered
	busy := runner.Run(accounts, rules)
	close(release)
	if busy.Changed || busy.More || len(busy.Errors) != 0 {
		t.Fatalf("busy invocation was not skipped: %+v", busy)
	}
	if result := <-done; len(result.Errors) != 0 {
		t.Fatal(result.Errors)
	}
}

func TestMailRulesLockHelperProcess(t *testing.T) {
	dir := os.Getenv("MATCHA_TEST_RULE_LOCK_DIR")
	if dir == "" {
		return
	}
	lock, acquired, err := lockProgress(dir)
	if err != nil || !acquired {
		t.Fatalf("child could not acquire lock: acquired=%t err=%v", acquired, err)
	}
	defer lock.Close() //nolint:errcheck
	fmt.Println("locked")
	var release [1]byte
	if _, err := os.Stdin.Read(release[:]); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerSkipsWhenAnotherProcessOwnsLock(t *testing.T) {
	home := ruleRunnerHome(t)
	command := exec.Command(os.Args[0], "-test.run=^TestMailRulesLockHelperProcess$")
	command.Env = append(os.Environ(), "MATCHA_TEST_RULE_LOCK_DIR="+filepath.Join(home, ".config", "matcha"))
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { command.Process.Kill() }) //nolint:errcheck
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatalf("child lock handshake: %q %v stderr=%s", line, err, stderr.String())
	}
	runner := NewRunner()
	runner.apply = func(*config.Account, []config.MailRule, fetcher.MailRuleProgress) (fetcher.MailRuleBatch, error) {
		t.Fatal("another process owns the rule runner lock")
		return fetcher.MailRuleBatch{}, nil
	}
	if result := runner.Run([]config.Account{{ID: "a"}}, []config.MailRule{runnerRule()}); result.Changed || result.More || len(result.Errors) != 0 {
		t.Fatalf("busy cross-process lock was not skipped: %+v", result)
	}
	if _, err := stdin.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("lock helper: %v stderr=%s", err, stderr.String())
	}
	lock, acquired, err := lockProgress(filepath.Join(home, ".config", "matcha"))
	if err != nil || !acquired {
		t.Fatalf("process exit did not release lock: %t %v", acquired, err)
	}
	lock.Close() //nolint:errcheck
}
