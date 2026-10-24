package fetcher

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/floatpane/matcha/config"
)

// MailRuleProgress is valid only for the account and rule definitions associated
// with it by the caller. LastUID bounds handled mail, including activation's
// historical baseline; actions occur only on subsequent higher UIDs.
type MailRuleProgress struct {
	UIDValidity uint32 `json:"uid_validity"`
	LastUID     uint32 `json:"last_uid"`
}

// MailRuleBatch preserves completed effects and the last safe checkpoint even
// when a later message or action fails. Processed never exceeds 100.
type MailRuleBatch struct {
	Progress  MailRuleProgress
	Changed   bool
	More      bool
	Processed int
}

// ApplyMailRules processes at most 100 current INBOX messages, oldest UID first.
// UID lookup, header-only fetching and all actions share one selected session.
// It never uses the client's potentially global-expunge MOVE fallback.
// New definitions and changed UIDVALIDITY only baseline the current UIDNEXT-1.
func ApplyMailRules(account *config.Account, rules []config.MailRule, progress MailRuleProgress) (MailRuleBatch, error) {
	batch := MailRuleBatch{Progress: progress}
	if account == nil {
		return batch, errors.New("mail rules require an account")
	}
	if err := config.ValidateMailRules(rules); err != nil {
		return batch, err
	}
	var applicable []config.MailRule
	hasActions := false
	for _, rule := range rules {
		if rule.Enabled && (rule.AccountID == "" || rule.AccountID == account.ID) {
			applicable = append(applicable, rule)
			hasActions = hasActions || rule.MoveTo != "" || rule.MarkRead
		}
	}
	if !hasActions {
		return batch, nil
	}
	if account.Protocol != "" && account.Protocol != "imap" {
		return batch, fmt.Errorf("account %q: local mail rules require IMAP", account.ID)
	}
	operation := "connect/authenticate"
	var ruleID string
	var uid uint32
	err := withIMAPClient(account, func(c *imapclient.Client) error {
		// The pool may retry a broken idle connection. Retain completed progress
		// and the remaining budget, rather than replaying an already handled batch.
		return applyMailRulesOnClient(c, account, applicable, &batch, &operation, &ruleID, &uid)
	})
	if err != nil {
		batch.More = false // errors retry at the normal poll interval, not a busy loop
		// Do not expose authentication-helper output, server response text, host
		// URLs, or credential-bearing wrapped errors in user-visible results.
		context := fmt.Sprintf("account %q: %s", account.ID, operation)
		if ruleID != "" {
			context += fmt.Sprintf(" for rule %q", ruleID)
		}
		if uid != 0 {
			context += fmt.Sprintf(" (INBOX UID %d)", uid)
		}
		if errors.Is(err, errMailRuleMoveUnsupported) {
			return batch, fmt.Errorf("%s: server must support IMAP MOVE; no unsafe fallback was attempted", context)
		}
		return batch, fmt.Errorf("%s failed; check account connectivity, authentication, permissions and destination mailbox", context)
	}
	return batch, nil
}

var errMailRuleMoveUnsupported = errors.New("IMAP MOVE is required")

func applyMailRulesOnClient(c *imapclient.Client, account *config.Account, rules []config.MailRule, batch *MailRuleBatch, operation, ruleID *string, currentUID *uint32) error {
	*operation, *ruleID, *currentUID = "select INBOX", "", 0
	batch.More = false
	selected, err := c.Select("INBOX", nil).Wait()
	if err != nil {
		return err
	}
	if selected.UIDValidity == 0 || selected.UIDNext == 0 {
		return errors.New("server returned invalid INBOX UID metadata")
	}
	if selected.UIDValidity != batch.Progress.UIDValidity {
		// New definitions and a recreated mailbox start with future arrivals.
		// Never act on historical INBOX mail during activation or a UID reset.
		batch.Progress = MailRuleProgress{
			UIDValidity: selected.UIDValidity,
			LastUID:     uint32(selected.UIDNext) - 1,
		}
		return nil
	}
	if selected.NumMessages == 0 || batch.Progress.LastUID == ^uint32(0) {
		return nil
	}
	*operation = "find pending INBOX UIDs"
	var pending imap.UIDSet
	pending.AddRange(imap.UID(batch.Progress.LastUID+1), 0)
	found, err := c.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{pending}}, nil).Wait()
	if err != nil {
		return err
	}
	uids := found.AllUIDs()
	// A range ending in '*' can include the server's highest UID even when
	// it is below the requested start. Never process the checkpoint again.
	uids = slices.DeleteFunc(uids, func(uid imap.UID) bool { return uint32(uid) <= batch.Progress.LastUID })
	slices.Sort(uids)
	budget := 100 - batch.Processed
	if len(uids) > budget {
		batch.More = true
		uids = uids[:budget]
	}
	if len(uids) == 0 {
		return nil
	}
	*operation = "fetch INBOX envelopes"
	deliverySection := &imap.FetchItemBodySection{
		Specifier:    imap.PartSpecifierHeader,
		HeaderFields: deliveryHeaderNames,
		Peek:         true,
	}
	options := &imap.FetchOptions{UID: true, Envelope: true, Flags: true}
	if !account.CatchAll {
		options.BodySection = []*imap.FetchItemBodySection{deliverySection}
	}
	messages, err := c.Fetch(imap.UIDSetNum(uids...), options).Collect()
	if err != nil {
		return err
	}
	byUID := make(map[imap.UID]*imapclient.FetchMessageBuffer, len(messages))
	for _, message := range messages {
		byUID[message.UID] = message
	}
	addresses := newAccountAddresses(account)
	for _, uid := range uids {
		*currentUID, *ruleID = uint32(uid), ""
		message := byUID[uid]
		if message == nil || message.Envelope == nil {
			// It may have disappeared since SEARCH; leave it uncheckpointed.
			// The next SEARCH will either omit it or fetch it again.
			*operation = "fetch complete INBOX envelope"
			return errors.New("message disappeared or its envelope was not returned")
		}
		if !account.CatchAll {
			if len(message.BodySection) == 0 {
				*operation = "fetch delivery headers"
				return errors.New("delivery headers were not returned")
			}
			recipients := make([]string, 0, len(message.Envelope.To)+len(message.Envelope.Cc))
			for _, address := range message.Envelope.To {
				recipients = append(recipients, address.Addr())
			}
			for _, address := range message.Envelope.Cc {
				recipients = append(recipients, address.Addr())
			}
			if !messageMatchesAccount(message, recipients, addresses, false, deliverySection) {
				batch.Progress.LastUID = uint32(uid)
				batch.Processed++
				continue
			}
		}
		for _, rule := range rules {
			if !ruleMatchesEnvelope(rule, message.Envelope) {
				continue
			}
			*ruleID = rule.ID
			if rule.MoveTo != "" && !c.Caps().Has(imap.CapMove) {
				*operation = "move message"
				return errMailRuleMoveUnsupported
			}
			wanted := imap.UIDSetNum(uid)
			if rule.MarkRead && !hasSeenFlag(message.Flags) {
				*operation = "mark message read"
				if err := c.Store(wanted, &imap.StoreFlags{
					Op:     imap.StoreFlagsAdd,
					Silent: true,
					Flags:  []imap.Flag{imap.FlagSeen},
				}, nil).Close(); err != nil {
					return err
				}
				batch.Changed = true
			}
			if rule.MoveTo != "" {
				*operation = "move message"
				if _, err := c.Move(wanted, rule.MoveTo).Wait(); err != nil {
					return err
				}
				batch.Changed = true
			}
			break // first enabled, account-scoped matching rule wins
		}
		batch.Progress.LastUID = uint32(uid)
		batch.Processed++
	}
	return nil
}

func ruleMatchesEnvelope(rule config.MailRule, envelope *imap.Envelope) bool {
	if !rule.Enabled || envelope == nil {
		return false
	}
	return ruleContainsAddresses(envelope.From, rule.From) &&
		(rule.To == "" || ruleContainsAddresses(envelope.To, rule.To) || ruleContainsAddresses(envelope.Cc, rule.To)) &&
		(rule.Subject == "" || strings.Contains(strings.ToLower(decodeHeader(envelope.Subject)), strings.ToLower(rule.Subject)))
}

func ruleContainsAddresses(addresses []imap.Address, condition string) bool {
	if condition == "" {
		return true
	}
	condition = strings.ToLower(condition)
	for _, address := range addresses {
		if strings.Contains(strings.ToLower(formatAddress(address)), condition) {
			return true
		}
	}
	return false
}
