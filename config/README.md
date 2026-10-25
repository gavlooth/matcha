# config

The `config` package handles all persistent application state: user configuration, email/contacts/drafts caching, folder caching, email signatures, and optional at-rest encryption. Configuration is stored under `~/.config/matcha/`, cache data under `~/.cache/matcha/`.

## Architecture

This package acts as the data layer for Matcha. It manages:

- **Account configuration** with multi-account support (Gmail, Outlook, iCloud, custom IMAP/SMTP)
- **Secure credential storage** via the OS keyring (with automatic migration from plain-text passwords), or inside the encrypted config when encryption is enabled
- **Local caches** for emails, email bodies, contacts, drafts, and folder listings to enable fast startup and offline browsing
- **Email body caching** for instant display of previously viewed emails without network round-trips
- **Email signatures** stored as plain text
- **Optional encryption** of all data files using AES-256-GCM with Argon2id key derivation

All files use JSON serialization with restrictive file permissions (`0600`/`0700`). When encryption is enabled, all files (except `secure.meta`) are encrypted before writing to disk.

## Storage Layout

Configuration (`~/.config/matcha/`):

| Path | Description |
|------|-------------|
| `config.json` | Account settings, preferences |
| `signature.txt` | Email signature |
| `secure.meta` | Encryption metadata (salt + sentinel), only present when encryption is enabled |
| `pgp/` | PGP keys |
| `plugins/` | Installed Lua plugins |
| `themes/` | Custom theme JSON files |

Cache (`~/.cache/matcha/`):

| Path | Description |
|------|-------------|
| `email_cache.json` | Email metadata cache |
| `contacts.json` | Contact autocomplete data |
| `drafts.json` | Saved email drafts |
| `folder_cache.json` | Folder listings per account |
| `search_history.json` | Recent searches from the search box (last 50) |
| `folder_emails/` | Per-folder email list cache |
| `email_bodies/` | Cached email body content and attachment metadata |

On startup, `MigrateCacheFiles()` moves any cache files from the old location (`~/.config/matcha/`) to `~/.cache/matcha/`.

## Composer Preferences

`composer_vim_mode` enables in-place Vim editing for the composer body. It defaults to `false` and is omitted from `config.json` while disabled. Recipient and subject fields keep their regular editing behavior. Change the saved default under **Settings → General → Vim mode in composer**. The **[ Vim: off/on ]** button beside Send toggles the current composer by click or Tab then Enter/Space; **Ctrl+G** and **F4** are alternatives. These per-message toggles do not change the saved preference, which is preserved in both plain and encrypted configuration.

## Contact Autocomplete

`SearchContactsForAccount` searches the secure contacts cache and the current
`mailing_lists` in `config.json`. Mailing lists remain virtual contacts with
expanded `Addresses`, ahead of regular contacts; regular contacts are filtered
and ranked using the selected account's usage. Results are limited to five.

Autocomplete reads only mailing-list metadata through `SecureReadFile`, so it
supports both plain and encrypted configuration without executing `pass_cmd`,
accessing the OS keyring, migrating credentials, or reloading keybinds on each
recipient edit. Missing or unreadable/invalid mailing-list configuration omits
lists but still returns cached contacts. If the contacts cache cannot be loaded,
search returns no suggestions, as before.

The metadata is read on each search rather than cached, so saved list changes
are visible immediately without introducing cache invalidation or changing the
search API.

## Files

| File | Description |
|------|-------------|
| `config.go` | Core configuration types (`Account`, `Config`, `MailingList`) and functions for loading, saving, and managing accounts. Handles IMAP/SMTP server resolution per provider, OS keyring integration, legacy config migration, and cache directory management (`cacheDir()`, `MigrateCacheFiles()`). |
| `cache.go` | Email, contacts, drafts, and email body caching. Provides CRUD operations for `EmailCache`, `ContactsCache` (with search and frequency-based ranking), `DraftsCache` (with save/delete/get operations), and `EmailBodyCache` (per-folder body + attachment metadata caching with pruning). |
| `folder_cache.go` | Caches IMAP folder listings per account and per-folder email metadata. Stores folder names to avoid repeated IMAP `LIST` commands, and caches email headers per folder for fast navigation. |
| `search_history.go` | Saves recent search queries, newest first and at most 50, in `search_history.json`. |
| `encryption.go` | Optional at-rest encryption using AES-256-GCM with Argon2id key derivation. Provides `SecureReadFile`/`SecureWriteFile` (transparent encryption wrappers used by all other files), `EnableSecureMode`/`DisableSecureMode`, password verification via an encrypted sentinel phrase, and session key management. |
| `signature.go` | Loads and saves the user's email signature from `~/.config/matcha/signature.txt`. |
| `oauth.go` | OAuth2 integration — token retrieval, authorization flow launcher, and embedded Python helper extraction. |
| `oauth_script.py` | Embedded OAuth2 helper script supporting Gmail, Outlook and Yahoo (browser-based auth, token refresh, secure storage). |
| `config_test.go` | Unit tests for configuration logic. |

## Encryption

When enabled via Settings, all data files are encrypted at rest using a user-chosen password. The password is never stored anywhere.

### How it works

1. **Key derivation**: The password is combined with a random 256-bit salt using Argon2id (time=3, memory=64MB, threads=4) to produce a 256-bit AES key.
2. **Sentinel verification**: A known phrase (`"matcha-verified"`) is encrypted with the derived key and stored in `secure.meta`. On login, if decrypting the sentinel succeeds, the password is correct.
3. **Transparent I/O**: All file read/write operations go through `SecureReadFile`/`SecureWriteFile`. When a session key is set, these encrypt/decrypt automatically. When no key is set (encryption disabled), they pass through to plain `os.ReadFile`/`os.WriteFile`.
4. **Password storage**: When encryption is active, account passwords are stored inside the encrypted `config.json` (via `secureDiskAccount`) instead of the OS keyring. When encryption is disabled, passwords are restored to the keyring.

### `secure.meta` format

```json
{
  "salt": "<base64-encoded-32-byte-salt>",
  "sentinel": "<base64-encoded-AES-GCM-encrypted-sentinel>",
  "argon2_time": 3,
  "argon2_memory": 65536,
  "argon2_threads": 4
}
```

This file is never encrypted itself — its existence signals that encryption is enabled.

## Email Body Cache

Email bodies and attachment metadata are cached per-folder in `~/.cache/matcha/email_bodies/`. When a user views an email:

1. The cache is checked first (`GetCachedEmailBody`).
2. If found, the cached body is returned instantly without a network call.
3. If not found, the body is fetched from the server and saved to cache (`SaveEmailBody`).
4. When folder emails are refreshed, stale body cache entries (for emails no longer on the server) are pruned (`PruneEmailBodyCache`).

Attachment binary data is not cached — only metadata (filename, MIME type, part ID, etc.) is stored. Attachment downloads always go to the server.

## OAuth2 / XOAUTH2

Accounts with `auth_method: "oauth2"` use the XOAUTH2 mechanism instead of passwords. This is supported for Gmail, Outlook and Yahoo. The flow works across three layers:

1. **`config/oauth.go`** — Go-side orchestration. Extracts the embedded Python helper to `~/.config/matcha/oauth/` (only when it changed, through an atomic rename), builds the interactive browser sign-in command (`Account.OAuth2SignInCommand()`, run by the TUI with the terminal attached), and returns fresh access tokens through `Account.OAuth2AccessToken()`. When an account sets `oauth_token_cmd`, that shell command supplies the access token instead of the Python helper. The `IsOAuth2()` method on `Account` checks the auth method.

2. **`config/oauth_script.py`** — Embedded Python script that handles the full OAuth2 lifecycle for Gmail, Outlook and Yahoo:
   - `auth` — Opens a browser for authorization with PKCE, using the provider's app registration from `--client-id`/`--client-secret`, `~/.config/matcha/oauth_apps.json` (local, never committed) or `oauth_client*.json`, in that order. An `http` redirect on `127.0.0.1`/`localhost` is served by a local listener (a free port when none is given; registrations from `--client-id` or `oauth_client*.json` use `localhost:8189`); any other redirect address is pasted back by the user. Tokens, together with the client that issued them, are saved to `~/.config/matcha/oauth_tokens/`. The provider is auto-detected from the email domain or can be specified with `--provider`.
   - `token` — Returns a fresh access token, automatically refreshing if expired (with a 5-minute buffer). A per-account file lock keeps concurrent requests from refreshing at the same time.
   - `revoke` — Revokes tokens and deletes local storage.
   - The user's own client credentials are stored per provider: `~/.config/matcha/oauth_client.json` (Gmail), `~/.config/matcha/oauth_client_<provider>.json` (others).

3. **`fetcher/xoauth2.go`** — Implements the XOAUTH2 SASL mechanism (`sasl.Client` interface) for IMAP/SMTP authentication. Formats the initial response as `user=<email>\x01auth=Bearer <token>\x01\x01` per the XOAUTH2 protocol spec.
