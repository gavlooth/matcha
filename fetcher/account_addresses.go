package fetcher

import (
	"bufio"
	"bytes"
	"log"
	"net/textproto"
	"slices"
	"strings"

	"github.com/floatpane/matcha/config"
)

// deliveryHeaderNames are the headers some servers add to name the address a
// message was delivered to, for example when it was forwarded or sent in Bcc.
var deliveryHeaderNames = []string{"Delivered-To", "X-Forwarded-To", "X-Original-To"}

// accountAddresses decides which messages of a mailbox belong to one account:
// mail sent to one of its addresses, unaddressed mail the user opts to show,
// mail matched by a visibility rule, and mail from senders the user always
// wants to see there.
type accountAddresses struct {
	account          *config.Account
	own              []string
	other            []string
	alwaysShowFrom   []string
	showRules        []config.MailRule
	showRuleSettings []string
}

func newAccountAddresses(account *config.Account) accountAddresses {
	addresses := accountAddresses{account: account}
	fetchEmail := account.FetchEmail
	if strings.TrimSpace(fetchEmail) == "" {
		fetchEmail = account.Email
	}
	for _, address := range append([]string{fetchEmail}, account.ExtraAddresses...) {
		if address = bareAddress(address); address != "" && !slices.Contains(addresses.own, address) {
			addresses.own = append(addresses.own, address)
		}
	}
	if account.ShowUnaddressedMail {
		for _, address := range account.OtherAccountAddresses {
			if address = bareAddress(address); address != "" && !slices.Contains(addresses.other, address) {
				addresses.other = append(addresses.other, address)
			}
		}
	}
	for _, sender := range account.AlwaysShowMailFrom {
		sender = strings.TrimPrefix(bareAddress(sender), "@")
		if sender != "" && !slices.Contains(addresses.alwaysShowFrom, sender) {
			addresses.alwaysShowFrom = append(addresses.alwaysShowFrom, sender)
		}
	}
	rules, err := config.LoadMailRules()
	if err != nil {
		log.Printf("cannot load show-in-account rules: %v", err)
	} else {
		for _, rule := range rules {
			if !rule.Enabled || !rule.ShowInAccount || rule.AccountID != "" && rule.AccountID != account.ID {
				continue
			}
			addresses.showRules = append(addresses.showRules, rule)
			addresses.showRuleSettings = append(addresses.showRuleSettings, showRuleDescription(rule))
		}
		slices.Sort(addresses.showRuleSettings)
	}
	return addresses
}

func showRuleDescription(rule config.MailRule) string {
	return strings.Join([]string{
		strings.TrimSpace(rule.ID),
		strings.ToLower(strings.TrimSpace(rule.From)),
		strings.ToLower(strings.TrimSpace(rule.To)),
		strings.ToLower(strings.TrimSpace(rule.Subject)),
	}, "|")
}

// AddressSettings describes the account settings that decide which messages
// of a mailbox the account shows. Saved header copies remember it, so a copy
// made with other settings is fetched again.
func AddressSettings(account *config.Account) string {
	if account.CatchAll {
		return "catch-all"
	}
	addresses := newAccountAddresses(account)
	own := slices.Sorted(slices.Values(addresses.own))
	showFrom := slices.Sorted(slices.Values(addresses.alwaysShowFrom))
	settings := "own=" + strings.Join(own, ",") + ";always-show-from=" + strings.Join(showFrom, ",")
	if isGmailAccount(account) {
		settings += ";gmail"
	}
	if account.ShowUnaddressedMail {
		other := slices.Sorted(slices.Values(addresses.other))
		settings += ";show-unaddressed=true;other=" + strings.Join(other, ",")
	}
	if len(addresses.showRuleSettings) > 0 {
		settings += ";show-rules=" + strings.Join(addresses.showRuleSettings, ";")
	}
	return settings
}

// bareAddress returns the lowercased address of "Name <user@host>",
// "<user@host>" or "user@host".
func bareAddress(value string) string {
	value = strings.TrimSpace(value)
	if open := strings.LastIndex(value, "<"); open >= 0 {
		if length := strings.Index(value[open:], ">"); length > 0 {
			value = value[open+1 : open+length]
		}
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func isGmailAccount(account *config.Account) bool {
	return account != nil && strings.EqualFold(account.ServiceProvider, "gmail")
}

// isOwn reports whether candidate is one of the account's addresses.
func (a accountAddresses) isOwn(candidate string) bool {
	candidate = bareAddress(candidate)
	for _, own := range a.own {
		if addressMatches(candidate, own, a.account) {
			return true
		}
	}
	return false
}

// namedInDeliveryHeaders reports whether a delivery header of the raw header
// block names one of the account's addresses.
func (a accountAddresses) namedInDeliveryHeaders(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	reader := textproto.NewReader(bufio.NewReader(bytes.NewReader(data)))
	headers, err := reader.ReadMIMEHeader()
	if err != nil && len(headers) == 0 {
		return false
	}
	for _, name := range deliveryHeaderNames {
		for _, value := range headers.Values(name) {
			for _, address := range strings.Split(value, ",") {
				if a.isOwn(address) {
					return true
				}
			}
		}
	}
	return false
}

// alwaysShowsMailFrom reports whether sender is listed in the account's
// "always show mail from" setting, by address or by domain. A domain entry
// also covers its subdomains: "uni.edu" covers "ee.uni.edu".
func (a accountAddresses) alwaysShowsMailFrom(sender string) bool {
	sender = bareAddress(sender)
	at := strings.LastIndex(sender, "@")
	if at < 0 {
		return false
	}
	domain := sender[at+1:]
	for _, entry := range a.alwaysShowFrom {
		if strings.Contains(entry, "@") {
			if sender == entry {
				return true
			}
		} else if domain == entry || strings.HasSuffix(domain, "."+entry) {
			return true
		}
	}
	return false
}
func (a accountAddresses) isOtherAccount(candidate string) bool {
	candidate = bareAddress(candidate)
	for _, other := range a.other {
		if addressMatches(candidate, other, a.account) {
			return true
		}
	}
	return false
}

func (a accountAddresses) namesNoConfiguredAddress(recipients []string, headers []byte) bool {
	for _, recipient := range recipients {
		if a.isOtherAccount(recipient) {
			return false
		}
	}
	return !a.namedInOtherDeliveryHeaders(headers)
}

func (a accountAddresses) namedInOtherDeliveryHeaders(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	reader := textproto.NewReader(bufio.NewReader(bytes.NewReader(data)))
	headers, err := reader.ReadMIMEHeader()
	if err != nil && len(headers) == 0 {
		return false
	}
	for _, name := range deliveryHeaderNames {
		for _, value := range headers.Values(name) {
			for _, address := range strings.Split(value, ",") {
				if a.isOtherAccount(address) {
					return true
				}
			}
		}
	}
	return false
}

// IsOwnAddress reports whether address belongs to the account, including its
// configured extra addresses.
func IsOwnAddress(account *config.Account, address string) bool {
	if account == nil {
		return false
	}
	candidate := bareAddress(address)
	fetchAddress := account.FetchEmail
	if strings.TrimSpace(fetchAddress) == "" {
		fetchAddress = account.Email
	}
	if addressMatches(candidate, bareAddress(fetchAddress), account) {
		return true
	}
	for _, extraAddress := range account.ExtraAddresses {
		if addressMatches(candidate, bareAddress(extraAddress), account) {
			return true
		}
	}
	return false
}
