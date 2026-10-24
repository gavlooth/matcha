package fetcher

import (
	"bufio"
	"bytes"
	"net/textproto"
	"slices"
	"strings"

	"github.com/floatpane/matcha/config"
)

// deliveryHeaderNames are the headers some servers add to name the address a
// message was delivered to, for example when it was forwarded or sent in Bcc.
var deliveryHeaderNames = []string{"Delivered-To", "X-Forwarded-To", "X-Original-To"}

// accountAddresses decides which messages of a mailbox belong to one account:
// mail sent to one of its own addresses, and mail from senders the user
// always wants to see there.
type accountAddresses struct {
	account *config.Account
	// own holds the fetch address and the extra addresses, lowercased.
	own []string
	// alwaysShowFrom holds sender addresses and domains, lowercased, without
	// a leading "@".
	alwaysShowFrom []string
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
	for _, sender := range account.AlwaysShowMailFrom {
		sender = strings.TrimPrefix(bareAddress(sender), "@")
		if sender != "" && !slices.Contains(addresses.alwaysShowFrom, sender) {
			addresses.alwaysShowFrom = append(addresses.alwaysShowFrom, sender)
		}
	}
	return addresses
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
