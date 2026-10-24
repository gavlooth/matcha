package fetcher

import (
	"cmp"
	"errors"
	"slices"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/floatpane/matcha/config"
)

// ErrHeaderSyncNotSupported is returned for accounts that are not plain IMAP
// (JMAP, POP3, Maildir); their mail is not copied into the local header index.
var ErrHeaderSyncNotSupported = errors.New("local header index needs an IMAP account")

// ErrFolderNeedsNewScan is returned by FetchMissingHeaders when the folder's
// UIDVALIDITY or the account's address settings changed since the scan; the
// caller must scan again.
var ErrFolderNeedsNewScan = errors.New("folder UIDs or the account's address settings changed since the last scan")

// FolderHeaders is what the local search index knows about one folder of one
// account. OtherUIDs are messages in the folder that the account does not show
// (see messageMatchesAccount); they are remembered so they are not fetched
// again. AddressSettings is AddressSettings(account) when the folder was
// scanned; a scan with other settings starts the folder over.
type FolderHeaders struct {
	UIDValidity     uint32
	AddressSettings string
	Emails          []Email
	OtherUIDs       []uint32
}

func headerSyncSupported(account *config.Account) bool {
	return !hasBackendProvider(account) && account.Protocol != "pop3"
}

// SyncFolderHeaders scans the folder once: it drops messages that are gone,
// refreshes read state, and fetches the headers of at most maxNew messages
// known does not have, newest first. It returns the updated headers and the
// UIDs still missing, newest first, for FetchMissingHeaders. The folder is
// opened read-only.
func SyncFolderHeaders(account *config.Account, folder string, known FolderHeaders, maxNew int) (FolderHeaders, []uint32, error) {
	if !headerSyncSupported(account) {
		return known, nil, ErrHeaderSyncNotSupported
	}
	var updated FolderHeaders
	var missing []uint32
	err := withIMAPClient(account, func(c *imapclient.Client) error {
		var err error
		updated, missing, err = scanFolderHeaders(c, account, folder, known, maxNew)
		return err
	})
	if err != nil {
		return known, nil, err
	}
	return updated, missing, nil
}

// FetchMissingHeaders fetches the headers of the next maxNew UIDs of missing
// (as returned by SyncFolderHeaders) into known, without scanning the whole
// folder again, and returns the UIDs still missing.
func FetchMissingHeaders(account *config.Account, folder string, known FolderHeaders, missing []uint32, maxNew int) (FolderHeaders, []uint32, error) {
	if !headerSyncSupported(account) {
		return known, nil, ErrHeaderSyncNotSupported
	}
	var updated FolderHeaders
	var rest []uint32
	err := withIMAPClient(account, func(c *imapclient.Client) error {
		var err error
		updated, rest, err = continueFolderHeaders(c, account, folder, known, missing, maxNew)
		return err
	})
	if err != nil {
		return known, nil, err
	}
	return updated, rest, nil
}

func continueFolderHeaders(c *imapclient.Client, account *config.Account, folder string, known FolderHeaders, missing []uint32, maxNew int) (FolderHeaders, []uint32, error) {
	selected, err := c.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return known, nil, err
	}
	if selected.UIDValidity != known.UIDValidity || known.AddressSettings != AddressSettings(account) {
		return known, nil, ErrFolderNeedsNewScan
	}
	fetchNow, later := splitBatch(missing, maxNew)
	updated, err := fetchHeaders(c, account, folder, known, fetchNow)
	if err != nil {
		return known, nil, err
	}
	return updated, later, nil
}

func scanFolderHeaders(c *imapclient.Client, account *config.Account, folder string, known FolderHeaders, maxNew int) (FolderHeaders, []uint32, error) {
	selected, err := c.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return known, nil, err
	}
	addressSettings := AddressSettings(account)
	if selected.UIDValidity != known.UIDValidity || known.AddressSettings != addressSettings {
		known = FolderHeaders{}
	}
	updated := FolderHeaders{UIDValidity: selected.UIDValidity, AddressSettings: addressSettings}
	if selected.NumMessages == 0 {
		return updated, nil, nil
	}

	// One UID FETCH 1:* (FLAGS) gives both the UIDs that still exist and their read state.
	var everyUID imap.UIDSet
	everyUID.AddRange(1, 0)
	flagged, err := c.Fetch(everyUID, &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil {
		return known, nil, err
	}
	readByUID := make(map[uint32]bool, len(flagged))
	for _, msg := range flagged {
		readByUID[uint32(msg.UID)] = hasSeenFlag(msg.Flags)
	}

	seen := make(map[uint32]bool, len(known.Emails)+len(known.OtherUIDs))
	for _, email := range known.Emails {
		if read, ok := readByUID[email.UID]; ok {
			email.IsRead = read
			updated.Emails = append(updated.Emails, email)
			seen[email.UID] = true
		}
	}
	for _, uid := range known.OtherUIDs {
		if _, ok := readByUID[uid]; ok {
			updated.OtherUIDs = append(updated.OtherUIDs, uid)
			seen[uid] = true
		}
	}

	var missing []uint32
	for uid := range readByUID {
		if !seen[uid] {
			missing = append(missing, uid)
		}
	}
	slices.SortFunc(missing, func(a, b uint32) int { return cmp.Compare(b, a) })
	fetchNow, later := splitBatch(missing, maxNew)
	updated, err = fetchHeaders(c, account, folder, updated, fetchNow)
	if err != nil {
		return known, nil, err
	}
	return updated, later, nil
}

func splitBatch(uids []uint32, n int) (now, later []uint32) {
	if len(uids) <= n {
		return uids, nil
	}
	return uids[:n], uids[n:]
}

// fetchHeaders adds the headers of uids to headers. UIDs the account does not
// show, or gone from the server meanwhile, go to OtherUIDs.
func fetchHeaders(c *imapclient.Client, account *config.Account, folder string, headers FolderHeaders, uids []uint32) (FolderHeaders, error) {
	if len(uids) == 0 {
		return headers, nil
	}
	var wanted imap.UIDSet
	for _, uid := range uids {
		wanted.AddNum(imap.UID(uid))
	}
	deliveryHeaderSection := buildDeliveryHeaderSection()
	msgs, err := c.Fetch(wanted, &imap.FetchOptions{
		Envelope:    true,
		UID:         true,
		Flags:       true,
		BodySection: []*imap.FetchItemBodySection{deliveryHeaderSection},
	}).Collect()
	if err != nil {
		return headers, err
	}
	sentMailbox, err := getMailboxByAttr(c, imap.MailboxAttrSent)
	if err != nil {
		sentMailbox = getSentMailbox(account)
	}
	emails := filterAndBuildBatchEmails(msgs, newAccountAddresses(account), folder == sentMailbox, deliveryHeaderSection)
	kept := make(map[uint32]bool, len(emails))
	for _, email := range emails {
		kept[email.UID] = true
	}
	updated := FolderHeaders{
		UIDValidity:     headers.UIDValidity,
		AddressSettings: headers.AddressSettings,
		Emails:          append(slices.Clip(headers.Emails), emails...),
		OtherUIDs:       slices.Clip(headers.OtherUIDs),
	}
	for _, uid := range uids {
		if !kept[uid] {
			updated.OtherUIDs = append(updated.OtherUIDs, uid)
		}
	}
	return updated, nil
}
