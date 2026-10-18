#!/usr/bin/env python3
"""
OAuth2 helper for Matcha email client.

Handles the full OAuth2 flow for Gmail, Outlook and Yahoo:
  - Browser-based authorization (PKCE) with an app registration from
    ~/.config/matcha/oauth_apps.json, the command line, or oauth_client*.json
  - Localhost callback server for auth code capture
  - Token exchange and refresh
  - Secure token storage in ~/.config/matcha/oauth_tokens/

Usage:
  oauth.py auth   <email> [--provider gmail|outlook|yahoo] [--client-id ID --client-secret SECRET]
  oauth.py token  <email>
  oauth.py revoke <email>

The 'auth' command initiates the OAuth2 flow, opening a browser.
The 'token' command prints a fresh access token to stdout (refreshing if needed).
The 'revoke' command deletes stored tokens for the given account.
"""

import argparse
import base64
import contextlib
import hashlib
import html
import http.server
import json
import os
import secrets
import sys
import threading
import time
import urllib.parse
import urllib.request
import webbrowser

try:
    import fcntl
except ImportError:  # Windows: no advisory locks; refreshes are not serialized.
    fcntl = None

# --- Provider configuration ---

PROVIDERS = {
    "gmail": {
        "name": "Gmail",
        "auth_endpoint": "https://accounts.google.com/o/oauth2/v2/auth",
        "token_endpoint": "https://oauth2.googleapis.com/token",
        "revoke_endpoint": "https://oauth2.googleapis.com/revoke",
        "scopes": ["https://mail.google.com/"],
        "extra_auth_params": {
            "access_type": "offline",
            "prompt": "consent",
        },
    },
    "outlook": {
        "name": "Outlook",
        "auth_endpoint": "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
        "token_endpoint": "https://login.microsoftonline.com/common/oauth2/v2.0/token",
        "revoke_endpoint": None,  # Microsoft does not support token revocation via endpoint
        "scopes": [
            "https://outlook.office.com/IMAP.AccessAsUser.All",
            "https://outlook.office.com/SMTP.Send",
            "offline_access",
        ],
        "extra_auth_params": {
            "prompt": "consent",
        },
    },
    "yahoo": {
        "name": "Yahoo",
        "auth_endpoint": "https://api.login.yahoo.com/oauth2/request_auth",
        "token_endpoint": "https://api.login.yahoo.com/oauth2/get_token",
        "revoke_endpoint": None,
        "scopes": ["mail-w"],
        "extra_auth_params": {},
    },
}

# Redirect URI for app registrations created by the user (see the setup guides).
USER_APP_REDIRECT_URI = "http://localhost:8189"


class AppRegistration:
    """An OAuth client registration to sign in with.

    redirect_uri decides how the browser returns: an http address on 127.0.0.1
    or localhost is served by a local listener (a free port when it names no
    port); any other address is copied from the browser and pasted back.
    save_as_user_credentials marks credentials passed on the command line,
    which are remembered in oauth_client*.json like before.
    """

    def __init__(self, client_id, client_secret, redirect_uri, save_as_user_credentials=False):
        self.client_id = client_id
        self.client_secret = client_secret or None
        self.redirect_uri = redirect_uri
        self.save_as_user_credentials = save_as_user_credentials


def app_registrations_file():
    """Return the path of the local, never-committed app registrations file."""
    return os.path.join(os.path.expanduser("~"), ".config", "matcha", "oauth_apps.json")


def find_app_registration(provider, cli_client_id=None, cli_client_secret=None):
    """Pick the app registration to sign in with, or None when there is none.

    Order: --client-id/--client-secret, then the provider's entry in
    ~/.config/matcha/oauth_apps.json, then the stored oauth_client*.json.
    """
    if cli_client_id:
        return AppRegistration(
            cli_client_id, cli_client_secret, USER_APP_REDIRECT_URI, save_as_user_credentials=True
        )
    path = app_registrations_file()
    if os.path.exists(path):
        with open(path, "r") as f:
            entry = json.load(f).get(provider)
        if entry and entry.get("client_id") and entry.get("redirect_uri"):
            return AppRegistration(
                entry["client_id"], entry.get("client_secret"), entry["redirect_uri"]
            )
    client_id, client_secret = load_client_credentials(provider)
    if client_id:
        return AppRegistration(client_id, client_secret, USER_APP_REDIRECT_URI)
    return None


def print_app_registration_help(provider):
    """Explain how to supply an app registration for the provider."""
    name = PROVIDERS[provider]["name"]
    print(f"No OAuth app registration for {name} is configured.", file=sys.stderr)
    print("Add one to " + app_registrations_file() + ", for example:", file=sys.stderr)
    print(
        f'  {{"{provider}": {{"client_id": "ID", "client_secret": "SECRET or omit",'
        ' "redirect_uri": "http://127.0.0.1"}}}}',
        file=sys.stderr,
    )
    print(
        "or pass --client-id and --client-secret (see the setup guides).",
        file=sys.stderr,
    )


def get_token_dir():
    """Return the token storage directory, creating it if needed."""
    home = os.path.expanduser("~")
    token_dir = os.path.join(home, ".config", "matcha", "oauth_tokens")
    os.makedirs(token_dir, mode=0o700, exist_ok=True)
    return token_dir


def token_file_for(email):
    """Return the token file path for a given email address."""
    safe_name = hashlib.sha256(email.encode()).hexdigest()[:16]
    return os.path.join(get_token_dir(), f"{safe_name}.json")


def load_tokens(email):
    """Load stored tokens for the given email, or return None."""
    path = token_file_for(email)
    if not os.path.exists(path):
        return None
    with open(path, "r") as f:
        return json.load(f)


def save_tokens(email, tokens):
    """Save tokens with restrictive permissions, replacing the file atomically."""
    path = token_file_for(email)
    tmp = f"{path}.{os.getpid()}.tmp"
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(tokens, f, indent=2)
    os.replace(tmp, path)


@contextlib.contextmanager
def token_lock(email):
    """Hold an exclusive lock on the account's tokens while reading or refreshing.

    Matcha requests tokens for several connections at once; without the lock
    they would all refresh together and race on rotated refresh tokens.
    """
    if fcntl is None:
        yield
        return
    path = token_file_for(email) + ".lock"
    fd = os.open(path, os.O_RDWR | os.O_CREAT, 0o600)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX)
        yield
    finally:
        os.close(fd)


def client_credentials_file_for(provider):
    """Return the client credentials file path for a given provider."""
    home = os.path.expanduser("~")
    if provider == "gmail":
        # Keep backwards-compatible path for Gmail
        return os.path.join(home, ".config", "matcha", "oauth_client.json")
    return os.path.join(home, ".config", "matcha", f"oauth_client_{provider}.json")


def load_client_credentials(provider):
    """Load OAuth2 client credentials for the given provider."""
    path = client_credentials_file_for(provider)
    if not os.path.exists(path):
        # Also try the generic path as fallback
        home = os.path.expanduser("~")
        generic = os.path.join(home, ".config", "matcha", "oauth_client.json")
        if provider != "gmail" and os.path.exists(generic):
            with open(generic, "r") as f:
                data = json.load(f)
            # Only use generic if it has provider-specific keys
            pid = data.get("provider")
            if pid == provider:
                return data.get("client_id"), data.get("client_secret")
        if not os.path.exists(path):
            return None, None
    with open(path, "r") as f:
        data = json.load(f)
    return data.get("client_id"), data.get("client_secret")


def save_client_credentials(provider, client_id, client_secret):
    """Save OAuth2 client credentials for the given provider."""
    path = client_credentials_file_for(provider)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump({"client_id": client_id, "client_secret": client_secret}, f, indent=2)


def post_form(url, params):
    """POST form-encoded params and return the decoded JSON response."""
    data = urllib.parse.urlencode(params).encode()
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read().decode())


def exchange_code(code, client_id, client_secret, provider, redirect_uri, verifier):
    """Exchange an authorization code for tokens."""
    params = {
        "code": code,
        "client_id": client_id,
        "redirect_uri": redirect_uri,
        "grant_type": "authorization_code",
        "code_verifier": verifier,
    }
    if client_secret:
        params["client_secret"] = client_secret
    try:
        return post_form(PROVIDERS[provider]["token_endpoint"], params)
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        print(f"Token exchange failed (HTTP {e.code}):", file=sys.stderr)
        print(body, file=sys.stderr)
        sys.exit(1)


def refresh_access_token(refresh_token, client_id, client_secret, provider):
    """Use a refresh token to get a new access token."""
    cfg = PROVIDERS[provider]
    params = {
        "refresh_token": refresh_token,
        "client_id": client_id,
        "grant_type": "refresh_token",
    }
    if client_secret:
        params["client_secret"] = client_secret
    # Outlook requires scope on refresh
    if provider == "outlook":
        params["scope"] = " ".join(cfg["scopes"])
    return post_form(cfg["token_endpoint"], params)


def revoke_token(token, provider):
    """Revoke an OAuth2 token."""
    cfg = PROVIDERS[provider]
    if cfg["revoke_endpoint"] is None:
        return False

    data = urllib.parse.urlencode({"token": token}).encode()
    req = urllib.request.Request(cfg["revoke_endpoint"], data=data, method="POST")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")

    try:
        with urllib.request.urlopen(req) as resp:
            return resp.status == 200
    except urllib.error.HTTPError:
        return False


class OAuthCallbackHandler(http.server.BaseHTTPRequestHandler):
    """HTTP handler that captures the OAuth2 redirect's query parameters."""

    params = None

    def do_GET(self):
        query = urllib.parse.urlparse(self.path).query
        params = {k: v[0] for k, v in urllib.parse.parse_qs(query).items()}
        if "code" not in params and "error" not in params:
            self.send_response(404)
            self.end_headers()
            return
        OAuthCallbackHandler.params = params
        ok = "code" in params
        self.send_response(200 if ok else 400)
        self.send_header("Content-Type", "text/html")
        self.end_headers()
        title = "Signed in" if ok else "Sign-in failed"
        detail = (
            "You can close this window and return to Matcha."
            if ok
            else f"Error: {html.escape(params['error'])}"
        )
        self.wfile.write(
            f"""
            <html><body style="font-family: sans-serif; text-align: center; padding-top: 50px;">
            <h2>{title}</h2>
            <p>{detail}</p>
            </body></html>
            """.encode()
        )

    def log_message(self, format, *args):
        """Suppress HTTP server logs."""
        pass


def detect_provider(email):
    """Detect the OAuth2 provider from an email address."""
    domain = email.rsplit("@", 1)[-1].lower() if "@" in email else ""
    outlook_domains = {
        "outlook.com",
        "hotmail.com",
        "live.com",
        "msn.com",
        "outlook.co.uk",
        "hotmail.co.uk",
        "live.co.uk",
        "outlook.de",
        "hotmail.de",
        "outlook.fr",
        "hotmail.fr",
        "outlook.it",
        "hotmail.it",
        "outlook.es",
        "hotmail.es",
        "outlook.jp",
        "hotmail.co.jp",
    }
    if domain in ("gmail.com", "googlemail.com"):
        return "gmail"
    if domain in outlook_domains:
        return "outlook"
    if domain.split(".", 1)[0] in ("yahoo", "ymail", "rocketmail"):
        return "yahoo"
    return None


def pkce_pair():
    """Return a PKCE (RFC 7636) code verifier and its S256 challenge."""
    verifier = secrets.token_urlsafe(64)
    digest = hashlib.sha256(verifier.encode("ascii")).digest()
    challenge = base64.urlsafe_b64encode(digest).decode("ascii").rstrip("=")
    return verifier, challenge


def wait_for_loopback_callback(server, state):
    """Serve the redirect on the loopback listener until it carries our state."""
    server.timeout = 300
    while OAuthCallbackHandler.params is None:
        server.handle_request()
        params = OAuthCallbackHandler.params
        if params is not None and params.get("state") != state:
            OAuthCallbackHandler.params = None
    server.server_close()
    return OAuthCallbackHandler.params


def read_pasted_redirect(state):
    """Ask for the address the browser was redirected to and parse it."""
    print("", file=sys.stderr)
    print(
        "After you sign in, the browser shows a page that cannot be reached.",
        file=sys.stderr,
    )
    print(
        "Copy the whole address from the address bar and paste it here.",
        file=sys.stderr,
    )
    while True:
        print("Address: ", end="", file=sys.stderr, flush=True)
        line = sys.stdin.readline()
        if not line:
            print("Sign-in cancelled.", file=sys.stderr)
            sys.exit(1)
        query = urllib.parse.urlparse(line.strip()).query
        params = {k: v[0] for k, v in urllib.parse.parse_qs(query).items()}
        if params.get("state") == state and ("code" in params or "error" in params):
            return params
        print(
            "That address does not contain this sign-in's code; try again.",
            file=sys.stderr,
        )


def do_auth(email, provider, app):
    """Run the browser authorization flow and store the resulting tokens.

    app is the AppRegistration to sign in with (see find_app_registration).
    """
    cfg = PROVIDERS[provider]
    client_id, client_secret = app.client_id, app.client_secret

    server = None
    redirect = urllib.parse.urlparse(app.redirect_uri)
    if redirect.scheme == "http" and redirect.hostname in ("127.0.0.1", "localhost"):
        # Loopback redirect: listen there; without a port in the URI, use a free one.
        server = http.server.HTTPServer(
            (redirect.hostname, redirect.port or 0), OAuthCallbackHandler
        )
        redirect_uri = f"http://{redirect.hostname}:{server.server_port}"
    else:
        # Any other address cannot be served here; the user pastes it back.
        redirect_uri = app.redirect_uri

    state = secrets.token_urlsafe(32)
    verifier, challenge = pkce_pair()
    auth_params = {
        "client_id": client_id,
        "redirect_uri": redirect_uri,
        "response_type": "code",
        "scope": " ".join(cfg["scopes"]),
        "state": state,
        "login_hint": email,
        "code_challenge": challenge,
        "code_challenge_method": "S256",
    }
    auth_params.update(cfg["extra_auth_params"])
    auth_url = f"{cfg['auth_endpoint']}?{urllib.parse.urlencode(auth_params)}"

    OAuthCallbackHandler.params = None
    print(f"Opening the browser to sign in to {cfg['name']} as {email}...", file=sys.stderr)
    print("If the browser doesn't open, visit this URL:", file=sys.stderr)
    print(f"  {auth_url}", file=sys.stderr)
    webbrowser.open(auth_url)

    if server is not None:
        params = wait_for_loopback_callback(server, state)
    else:
        params = read_pasted_redirect(state)

    if "error" in params:
        print(f"Authorization error: {params['error']}", file=sys.stderr)
        sys.exit(1)

    print("Authorization code received, exchanging for tokens...", file=sys.stderr)
    token_response = exchange_code(
        params["code"], client_id, client_secret, provider, redirect_uri, verifier
    )
    if "error" in token_response:
        print(f"Token exchange error: {token_response['error']}", file=sys.stderr)
        sys.exit(1)

    tokens = {
        "access_token": token_response["access_token"],
        "refresh_token": token_response.get("refresh_token"),
        "expires_at": int(time.time()) + token_response.get("expires_in", 3600),
        "token_type": token_response.get("token_type", "Bearer"),
        "email": email,
        "provider": provider,
        "client_id": client_id,
        "client_secret": client_secret,
    }
    with token_lock(email):
        save_tokens(email, tokens)
    if app.save_as_user_credentials:
        save_client_credentials(provider, client_id, client_secret)

    print("Authorization complete! Tokens saved.", file=sys.stderr)


def do_token(email):
    """Get a fresh access token, refreshing if needed."""
    with token_lock(email):
        tokens = load_tokens(email)
        if tokens is None:
            print(
                f"Not signed in. Run: matcha oauth auth {email}", file=sys.stderr
            )
            sys.exit(1)

        provider = tokens.get("provider", "gmail")

        # Check if token is expired (with 5 minute buffer)
        if time.time() >= tokens.get("expires_at", 0) - 300:
            client_id = tokens.get("client_id")
            client_secret = tokens.get("client_secret")
            if not client_id:
                # Tokens from older versions rely on the stored app registration.
                client_id, client_secret = load_client_credentials(provider)
            if not client_id:
                print(
                    f"No client credentials found. Run: matcha oauth auth {email}",
                    file=sys.stderr,
                )
                sys.exit(1)

            refresh_token = tokens.get("refresh_token")
            if not refresh_token:
                print(
                    f"No refresh token available. Run: matcha oauth auth {email}",
                    file=sys.stderr,
                )
                sys.exit(1)

            try:
                new_tokens = refresh_access_token(
                    refresh_token, client_id, client_secret, provider
                )
            except urllib.error.HTTPError as e:
                print(
                    f"Token refresh failed ({e}). Run: matcha oauth auth {email}",
                    file=sys.stderr,
                )
                sys.exit(1)

            tokens["access_token"] = new_tokens["access_token"]
            tokens["expires_at"] = int(time.time()) + new_tokens.get("expires_in", 3600)
            # Refresh tokens may be rotated
            if "refresh_token" in new_tokens:
                tokens["refresh_token"] = new_tokens["refresh_token"]

            save_tokens(email, tokens)

    print(tokens["access_token"])


def do_revoke(email):
    """Revoke and delete stored tokens."""
    tokens = load_tokens(email)
    if tokens is None:
        print("No tokens found.", file=sys.stderr)
        sys.exit(1)

    provider = tokens.get("provider", "gmail")

    # Try to revoke the refresh token first, then access token
    revoked = False
    if tokens.get("refresh_token"):
        revoked = revoke_token(tokens["refresh_token"], provider)
    if not revoked and tokens.get("access_token"):
        revoked = revoke_token(tokens["access_token"], provider)

    # Delete local token file
    path = token_file_for(email)
    if os.path.exists(path):
        os.remove(path)

    if revoked:
        print("Token revoked and deleted.", file=sys.stderr)
    else:
        print(
            "Local tokens deleted (remote revocation may have failed).", file=sys.stderr
        )


def main():
    parser = argparse.ArgumentParser(description="OAuth2 helper for Matcha")
    subparsers = parser.add_subparsers(dest="command")

    # auth command
    auth_parser = subparsers.add_parser("auth", help="Authorize an email account")
    auth_parser.add_argument("email", help="Email address")
    auth_parser.add_argument(
        "--provider",
        help="OAuth2 provider (gmail, outlook or yahoo)",
        choices=sorted(PROVIDERS),
    )
    auth_parser.add_argument("--client-id", help="OAuth2 client ID")
    auth_parser.add_argument("--client-secret", help="OAuth2 client secret")

    # token command
    token_parser = subparsers.add_parser("token", help="Get a fresh access token")
    token_parser.add_argument("email", help="Email address")

    # revoke command
    revoke_parser = subparsers.add_parser("revoke", help="Revoke stored tokens")
    revoke_parser.add_argument("email", help="Email address")

    args = parser.parse_args()

    if args.command == "auth":
        provider = args.provider
        if not provider:
            provider = detect_provider(args.email)
        if not provider:
            print(
                "Error: Could not detect provider from email address.", file=sys.stderr
            )
            print("Use --provider gmail, outlook or yahoo", file=sys.stderr)
            sys.exit(1)

        app = find_app_registration(provider, args.client_id, args.client_secret)
        if app is None:
            print_app_registration_help(provider)
            sys.exit(1)
        do_auth(args.email, provider, app)

    elif args.command == "token":
        do_token(args.email)

    elif args.command == "revoke":
        do_revoke(args.email)

    else:
        parser.print_help()
        sys.exit(1)


if __name__ == "__main__":
    main()
