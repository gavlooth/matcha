package app

import (
	"errors"
	"log"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/internal/emailstore"
	"github.com/floatpane/matcha/internal/fetchcmd"
	"github.com/floatpane/matcha/internal/mailrules"
	"github.com/floatpane/matcha/tui"
)

const mailRuleInterval = 30 * time.Second

type mailRuleTickMsg struct{}

type mailRuleDoneMsg struct {
	result mailrules.Result
	rules  []config.MailRule
}

func nextMailRuleRun(after time.Duration) tea.Cmd {
	return tea.Tick(after, func(time.Time) tea.Msg { return mailRuleTickMsg{} })
}

func (m *Model) startMailRules() tea.Cmd {
	if m.mailRuleRunning {
		return nil
	}
	if m.config == nil || !m.config.HasAccounts() {
		return nextMailRuleRun(mailRuleInterval)
	}
	if _, locked := m.current.(*tui.PasswordPrompt); locked {
		return nextMailRuleRun(mailRuleInterval)
	}
	// Let a pending manual move finish its undo period before moving mail.
	if m.actionManager != nil && m.actionManager.IsPending() {
		return nextMailRuleRun(mailRuleInterval)
	}
	if m.mailRuleRunner == nil {
		m.mailRuleRunner = mailrules.NewRunner()
	}
	runner := m.mailRuleRunner
	accounts := slices.Clone(m.config.Accounts)
	m.mailRuleRunning = true
	return func() tea.Msg {
		rules, err := config.LoadMailRules()
		if err != nil {
			return mailRuleDoneMsg{result: mailrules.Result{Errors: []error{err}}}
		}
		return mailRuleDoneMsg{result: runner.Run(accounts, rules), rules: rules}
	}
}

func (m *Model) finishMailRules(msg mailRuleDoneMsg) tea.Cmd {
	m.mailRuleRunning = false
	wait := mailRuleInterval
	if msg.result.More {
		wait = time.Second
	}
	cmds := []tea.Cmd{nextMailRuleRun(wait)}
	errText := ""
	if err := errors.Join(msg.result.Errors...); err != nil {
		errText = "Automatic rules: " + err.Error()
	}
	if errText != "" && errText != m.mailRuleLastError {
		log.Print(errText)
		cmds = append(cmds, m.showErrorCmd(errText))
	}
	m.mailRuleLastError = errText
	if !msg.result.Changed || m.config == nil {
		return tea.Batch(cmds...)
	}

	// Cached folder pages no longer describe the server after a move.
	if m.store != nil {
		delete(m.store.FolderEmails, emailstore.FolderInbox)
		for _, rule := range msg.rules {
			if rule.Enabled && rule.MoveTo != "" {
				delete(m.store.FolderEmails, rule.MoveTo)
			}
		}
	}
	if m.folderInbox != nil {
		cmds = append(cmds,
			m.refreshFolderEmails(m.folderInbox.GetCurrentFolder()),
			fetchcmd.FetchFoldersCmd(m.config),
		)
	}
	if m.headerIndex != nil {
		for _, account := range m.config.Accounts {
			if m.headerIndex.StartSync(account.ID, emailstore.FolderInbox, 0) {
				cmds = append(cmds, m.headerSyncStepCmd(account, emailstore.FolderInbox))
			}
		}
	}
	return tea.Batch(cmds...)
}
