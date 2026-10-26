---
title: Usage
sidebar_position: 3
---

# Usage

## First Launch

On first launch, Matcha will prompt you to configure an email account. You'll need:

- Your email address
- Your password (or app-specific password for Gmail/iCloud)
- Email provider (Gmail, iCloud, or Custom)

## Keyboard Shortcuts

### Main Menu

- `↑/↓` or `j/k` - Navigate menu items
- `Enter` - Select option
- `H` - Open Hidden Mail for INBOX across all accounts
- `Esc` - Go back / Exit
- `Ctrl+C` - Quit application

Select **Hidden Mail** in the main menu (or press `H`) to open messages hidden by account-address filtering. This opens INBOX across all accounts; use the account tabs to narrow the list.

### Inbox View

- `↑/↓` or `j/k` - Navigate emails
- `←/→` or `h/l` - Switch between account tabs
- `Enter` - Open selected email
- `/` - Filter/search emails
- `r` - Refresh inbox
- `[` / `]` - Switch focus between inbox and split pane (when split pane is enabled)
- `d` - Delete selected email
- `a` - Archive selected email
- `v` - Enter visual mode (multi-select)
- `H` - Toggle hidden mail for the open folder and account tab
- `Esc` - Leave hidden mail/search first; otherwise return to the main menu

Press `H` to list hidden mail in the open folder for the selected account tab. Press `H` again or `Esc` to return to ordinary mail. Switching account tabs keeps the hidden-mail view open for the selected account; switching folders returns to ordinary mail, where `H` opens that folder's hidden messages. The inbox shows an `H` hint beside its hidden count and in the help bar. Hidden mail names none of that account's addresses. Open a message to read it normally. Add an address to **Extra Addresses**, add a sender or domain to **Always Show Mail From**, or create a show-in-account mail rule to make matching mail appear in the normal inbox.

In **Settings → Accounts**, select an account and press `a` to open **Suggested addresses** from hidden To/Cc recipients. They are not verified aliases: add only addresses that belong to you, not teachers, mailing lists or other recipients. Press `Enter` to add the highlighted address to **Extra Addresses**, `x` to ignore it, or `Esc` to return. Use **Always Show Mail From** or a show-in-account rule for Bcc course mail.

Each row shows the sender and subject. On an individual account tab, rows omit the account/recipient address prefix because the tab already identifies the account; this also applies to search results. In the combined view, address labels distinguish accounts and preserve the original recipient for extra addresses. Delivery headers are used when To/Cc do not identify one of the account's addresses. If Bcc delivery leaves no recipient information, the label falls back to the account's main address; it cannot establish which alias received that message. Aliases remain part of their account rather than separate tabs.

#### Visual Mode (Multi-Select)

Visual mode allows you to select multiple emails and perform batch operations, similar to Vim's visual mode:

- `v` - Enter visual mode (selects current email)
- `↑/↓` or `j/k` - Expand/contract selection
- `d` - Delete all selected emails
- `a` - Archive all selected emails
- `m` - Move all selected emails to a folder
- `v` or `Esc` - Exit visual mode

**Visual indicators:**
- `>` - Cursor position
- `*` - Selected email (not cursor)
- `>*` - Selected email with cursor
- Title shows: "Inbox - VISUAL (N selected)"

**Features:**
- Efficient batch operations using single IMAP commands
- Automatic prevention of cross-account selections
- Works in both inbox and folder views
- Visual mode disabled during search/filtering for safety

#### Search

`/` opens the search box. It searches in two places:

- **While you type**, Matcha searches the email headers it keeps on this computer (sender, recipients, subject, date) for the open folder and account tab. Matching ignores case and accents, finds parts of words (`verif` finds "verification") and allows skipped letters inside a word (`vrfctn` also finds it). Every word you type must match, in any order. The first time a folder is searched, its headers are copied in the background; the box shows how many are copied so far. INBOX is copied when Matcha starts.
- **Enter** sends the search to the mail server, which also looks in email bodies. How the server matches words depends on the provider: Gmail and Yahoo match whole words only.

In the result list, `↓` or `Tab` moves to the results, `j/k` or `↑/↓` pick one, `Enter` opens it, and `Tab` goes back to the query. A click picks a result and a double click opens it.

While the query is empty, the box lists your recent searches, newest first. Matcha saves a search when you press `Enter` to search the server, and when you open a result found while typing. It keeps the last 50; searching the same query again moves it to the top. Pick a recent search like a result (`↓` or `Tab`, then `j/k` or `↑/↓`, or a click and a second click): `Enter` puts it in the query and shows the matches on this computer, and a second `Enter` searches the server. `d` or `Delete` removes the highlighted search, `D` removes all of them. Recent searches are stored in `~/.cache/matcha/search_history.json` and follow Matcha's encryption setting.

Both searches accept plain words and `key:value` filters. Wrap values with spaces in quotes, e.g. `subject:"quarterly report"`.

- Plain words - match the sender, recipients or subject (the server also matches the body)
- `from:` / `to:` / `subject:` - match only that header
- `body:` - match only the message body (server only)
- `since:` / `before:` - date as `2026-01-31` or RFC 3339
- `larger:` - minimum size in bytes (server only)

Example: `from:alice invoice since:2026-01-01` finds mail from alice that mentions "invoice". Server searches of several accounts run at the same time, over connections that stay open for 5 minutes.

### Email View

- `↑/↓` or `j/k` - Scroll email content
- `r` - Reply to email
- `d` - Delete email
- `a` - Archive email
- `Tab` - Focus attachments
- `Esc` - Back to inbox
- `i` - Toggle images

### Attachment View (when focused)

- `↑/↓` or `j/k` - Navigate attachments
- `Enter` - Download and open attachment
- `Tab` or `Esc` - Back to email body

### Composer

- `Tab` / `Shift+Tab` - Navigate fields
- `Enter` -
  - On "From" field: Select account (if multiple)
  - On "Attachment" field: Open file picker
  - On "Send" button: Send email
- `↑/↓` - Navigate contact suggestions (when typing in "To" field)

- `Esc` - Save draft and exit

Replies use the configured extra address named in the original To/Cc or delivery headers; the main fetch address takes precedence when both are present. The selected address is preserved in drafts and queued sends. Switching accounts clears the reply override and uses the new account's sender.

#### Vim mode

Select **[ Vim: off/on ]** beside **Send** to toggle Vim-style body editing for that message: click it, or reach it with **Tab** and press **Enter** or **Space**. **Ctrl+G** and **F4** also toggle it. To enable it for new compositions, replies, forwards and reopened drafts, turn on **Settings → General → Vim mode in composer** (`composer_vim_mode: true` in configuration). It is off by default. Recipient, subject and signature fields remain ordinary inputs.

The footer shows **NORMAL**, **INSERT**, **VISUAL** or **VISUAL LINE** while the body is focused.

| Action | Keys |
| --- | --- |
| Insert before/after cursor; insert at first nonblank/end of line | `i` / `a`; `I` / `A` |
| Open a line below/above | `o` / `O` |
| Move through characters, lines or words | `h j k l`, arrows, `w b e` (`W B E` for whitespace-separated words) |
| Start/first nonblank/end of line; start/end of body | `0` / `^` / `$`; `gg` / `G` |
| Delete/change/yank a motion or whole lines | `d` / `c` / `y` plus a motion; `dd` / `cc` / `yy` |
| Delete/change/yank the inner word | `diw` / `ciw` / `yiw` |
| Delete character or through end of line | `x` / `X`; `D` (`C` changes through the line end) |
| Select characters or lines | `v` / `V`, then motions; `d` / `c` / `y` acts on the selection |
| Paste after/before cursor or replace a selection | `p` / `P` |
| Undo/redo; repeat last edit | `u` / `Ctrl+R`; `.` |

Counts work with motions and edits, including `5j`, `3dd` and `2d3w`. One insertion session is one undo step. Yanked/deleted text uses an internal register, not the system clipboard. Visual `p` retains the replaced selection in that register; visual `P` preserves the previous register.

**Esc in the body returns to Normal mode or cancels a pending command; it does not exit the composer.** `Tab`/`Shift+Tab` still move between fields. To save the draft and leave, move out of the body, then press `Esc`. `Ctrl+C` retains the existing save-on-quit behavior. Pasted text is inserted literally, never interpreted as Vim commands.

This is in-place Vim-style editing, not a complete Vim runtime: Ex commands, macros and Vim plugins are not supported. For full Vim/Neovim, `Ctrl+E` still opens your configured external editor and imports the resulting body when you return.

## Automatic Email Rules

Open **Settings → Rules** to manage local rules for your IMAP accounts. Rules run while the Matcha TUI is open, including when you are on the start menu or composing mail. They do not run in the background daemon after you close the TUI, and do not require Sieve or provider-specific authorization.

- Press `n` to create a rule, or select one and press `Enter` to edit it. New rules are disabled until you enable them.
- Choose all configured accounts or one account. Add sender, recipient or subject conditions: matching uses case-insensitive substrings, and every nonempty condition must match. The recipient condition includes To and Cc.
- Choose **Show in this account even when not addressed to it**, **Mark read**, a destination folder, or any combination. Show rules make matching mail visible in the selected account without moving or marking it; show-only rules do not contact the mail server. A rule that combines show with move or mark-read still performs those actions.
- Use `Tab`/`Shift+Tab` or `↑`/`↓` between form fields, `←`/`→` to choose an account, `Space`/`Enter` to toggle options, and `Ctrl+S` to save. `Esc` cancels editing.
- In the rule list, `Space` enables/disables, `d` asks before deleting, uppercase `K`/`J` changes order, and `r` reloads saved definitions. For move and mark-read processing, the **first enabled matching rule wins**, even if it only shows the message. Enabled show rules also determine visibility independently.

Move and mark-read actions apply to **future incoming INBOX mail**, not messages already in your inbox. New, edited, reordered or newly enabled rules establish their starting point at the next background check, at most 30 seconds after saving. Mail already present at that check is left alone. After this starting point, unchanged rules also process mail that arrives while Matcha is closed when you open it again. The show-in-account setting also applies to existing mail when the inbox and local header copy refresh, within 30 seconds while the TUI is open.

Each check processes at most 100 messages per applicable account, with further batches one second apart. Progress is saved between batches and across restarts. Multiple Matcha windows share a processing lock. Failed actions show an error and are retried at the normal 30-second interval without skipping the failed message; if marking read succeeds but moving fails, that message remains read.

Definitions are stored in `~/.config/matcha/mail_rules.json`; processing progress is in `~/.cache/matcha/mail_rules_progress.json`. Both follow Matcha's encryption setting. Use the Settings UI to edit rules so activation information is updated correctly. Clearing progress establishes a new starting point rather than replaying old messages.

If enabling encryption reports an existing-file migration failure, Matcha retains the vault metadata so files already encrypted remain recoverable with your password. Correct the reported file-access problem, then disable and re-enable encryption to finish migrating all files.

## CLI Commands

Matcha includes several CLI subcommands that work without launching the TUI.

### Send Email

Send an email directly from the command line:

```bash
matcha send --to user@example.com --subject "Hello" --body "Hi there"
```

This is useful for scripts, automation, and [AI agent integration](./Features/AI_AGENTS.md). See the full [CLI reference](./Features/CLI.md) for all options.

### Update

Check for updates and install the latest version:

```bash
matcha update
```

This command will:

1. Check for the latest release on GitHub
2. Detect your installation method (Homebrew, Snap, or binary)
3. Update using the appropriate method

### Gmail OAuth2

Manage Gmail OAuth2 authorization:

```bash
matcha gmail auth <email>     # Authorize a Gmail account
matcha gmail token <email>    # Print a fresh access token
matcha gmail revoke <email>   # Revoke stored tokens
```
