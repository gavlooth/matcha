package fetcher

import (
	"cmp"
	"slices"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/floatpane/matcha/config"
)

// DiscoveredAddress is an address with enough evidence in Sent and hidden mail
// to add it as an address of the account.
type DiscoveredAddress struct {
	Address                string
	SentCount, HiddenCount int
}

// DiscoverOwnAddresses finds addresses used both to send mail and as hidden
// INBOX recipients, at least three times each. Sent alone is not enough:
// services may store notifications under their own From address. Repeated use
// in both folders is a heuristic, not proof that the user owns the address.
func DiscoverOwnAddresses(account *config.Account, hiddenRecipients map[string]int) ([]DiscoveredAddress, error) {
	if account == nil || !headerSyncSupported(account) {
		return nil, nil
	}

	sentCounts := make(map[string]int)
	err := withIMAPClient(account, func(c *imapclient.Client) error {
		mailbox, err := getMailboxByAttr(c, imap.MailboxAttrSent)
		if err != nil {
			mailbox = getSentMailbox(account)
		}
		if _, err := c.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return err
		}
		var allMessages imap.UIDSet
		allMessages.AddRange(1, 0)
		messages, err := c.Fetch(allMessages, &imap.FetchOptions{Envelope: true}).Collect()
		if err != nil {
			return err
		}
		for _, message := range messages {
			if message.Envelope == nil {
				continue
			}
			seen := make(map[string]struct{}, len(message.Envelope.From))
			for _, from := range message.Envelope.From {
				address := bareAddress(from.Addr())
				if address == "" {
					continue
				}
				if _, duplicate := seen[address]; duplicate {
					continue
				}
				seen[address] = struct{}{}
				sentCounts[address]++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	addresses := newAccountAddresses(account)
	results := make([]DiscoveredAddress, 0)
	for address, sentCount := range sentCounts {
		hiddenCount := hiddenRecipients[strings.ToLower(address)]
		if sentCount < 3 || hiddenCount < 3 || addresses.isOwn(address) {
			continue
		}
		if isOtherAccountAddress(account, address) || isIgnoredAddress(account, address) {
			continue
		}
		results = append(results, DiscoveredAddress{Address: address, SentCount: sentCount, HiddenCount: hiddenCount})
	}
	slices.SortFunc(results, func(a, b DiscoveredAddress) int {
		if order := cmp.Compare(b.SentCount+b.HiddenCount, a.SentCount+a.HiddenCount); order != 0 {
			return order
		}
		return cmp.Compare(a.Address, b.Address)
	})
	return results, nil
}

func isOtherAccountAddress(account *config.Account, candidate string) bool {
	for _, address := range account.OtherAccountAddresses {
		if addressMatches(candidate, bareAddress(address), account) {
			return true
		}
	}
	return false
}

func isIgnoredAddress(account *config.Account, candidate string) bool {
	for _, address := range account.IgnoredAddresses {
		if addressMatches(candidate, bareAddress(address), account) {
			return true
		}
	}
	return false
}
