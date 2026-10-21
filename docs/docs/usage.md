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
