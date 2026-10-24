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
- `Esc` - Go back / Exit
- `Ctrl+C` - Quit application

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
- `Esc` - Back to main menu

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

## Automatic Email Rules

Open **Settings → Rules** to manage local rules for your IMAP accounts. Rules run while the Matcha TUI is open, including when you are on the start menu or composing mail. They do not run in the background daemon after you close the TUI, and do not require Sieve or provider-specific authorization.

- Press `n` to create a rule, or select one and press `Enter` to edit it. New rules are disabled until you enable them.
- Choose all configured accounts or one account. Add sender, recipient or subject conditions: matching uses case-insensitive substrings, and every nonempty condition must match. The recipient condition includes To and Cc.
- Choose **Mark read**, a destination folder, or both. The destination must already exist in each affected account; moving requires the server's IMAP MOVE capability. Matcha never uses a move fallback that could expunge unrelated messages.
- Use `Tab`/`Shift+Tab` or `↑`/`↓` between form fields, `←`/`→` to choose an account, `Space`/`Enter` to toggle options, and `Ctrl+S` to save. `Esc` cancels editing.
- In the rule list, `Space` enables/disables, `d` asks before deleting, uppercase `K`/`J` changes order, and `r` reloads saved definitions. The **first enabled matching rule wins**, even if it only marks the message read.

Rules apply to **future incoming INBOX mail**, not messages already in your inbox. New, edited, reordered or newly enabled rules establish their starting point at the next background check, at most 30 seconds after saving. Mail already present at that check is left alone. After this starting point, unchanged rules also process mail that arrives while Matcha is closed when you open it again.

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
