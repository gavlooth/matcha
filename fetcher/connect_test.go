package fetcher

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/floatpane/matcha/config"
)

func TestConnectClosesSocketOnLoginFailure(t *testing.T) {
	for _, tc := range []struct {
		name, authMethod, tokenCommand, password, wantError string
	}{
		{"password rejected", "", "", "wrong", "authentication error"},
		{"oauth rejected", "oauth2", "printf test-token", "", "XOAUTH2 authentication failed"},
		{"token command failed", "oauth2", "exit 1", "", "oauth2 token retrieval failed"},
		{"token command empty", "oauth2", "printf ''", "", "empty access token"},
		{"password missing", "", "", "", "no password available"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account, disconnected := loginTestServer(t, false)
			account.AuthMethod = tc.authMethod
			account.OAuthTokenCmd = tc.tokenCommand
			account.Password = tc.password
			account.PassCmd = "printf ''"

			c, err := connect(account)
			if c != nil {
				c.Close() //nolint:errcheck
				t.Fatal("failed login returned a client")
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("connect error = %v, want %q", err, tc.wantError)
			}
			select {
			case err := <-disconnected:
				if err != io.EOF {
					t.Fatalf("server read error = %v, want EOF from client closing socket", err)
				}
			case <-time.After(time.Second):
				t.Fatal("failed login left the IMAP socket open")
			}
		})
	}
}

func TestConnectKeepsAuthenticatedSocketOpen(t *testing.T) {
	account, disconnected := loginTestServer(t, true)
	account.Password = "secret"
	c, err := connect(account)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() }) //nolint:errcheck
	if err := c.Noop().Wait(); err != nil {
		t.Fatalf("authenticated client cannot run NOOP: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-disconnected:
		if err != io.EOF {
			t.Fatalf("server read error = %v, want EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("client Close left the IMAP socket open")
	}
}

// loginTestServer keeps rejected sessions open so the test observes whether
// the client closes its socket, rather than relying on server cleanup.
func loginTestServer(t *testing.T, acceptLogin bool) (*config.Account, <-chan error) {
	t.Helper()
	certServer := httptest.NewTLSServer(nil)
	certificate := certServer.TLS.Certificates[0]
	certServer.Close()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() }) //nolint:errcheck
	disconnected := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			disconnected <- err
			return
		}
		defer conn.Close()                                //nolint:errcheck
		conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
		fmt.Fprint(conn, "* OK [CAPABILITY IMAP4rev1 AUTH=XOAUTH2 SASL-IR] ready\r\n")
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				disconnected <- err
				return
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			tag, command := fields[0], strings.ToUpper(fields[1])
			switch command {
			case "CAPABILITY":
				fmt.Fprint(conn, "* CAPABILITY IMAP4rev1 AUTH=XOAUTH2 SASL-IR\r\n")
			case "LOGIN", "AUTHENTICATE":
				if !acceptLogin {
					fmt.Fprintf(conn, "%s NO [ALERT] Too many simultaneous connections. (Failure)\r\n", tag)
					continue
				}
			}
			fmt.Fprintf(conn, "%s OK done\r\n", tag)
		}
	}()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return &config.Account{
		ID: "login-test", Email: "me@example.com", ServiceProvider: config.ProviderCustom,
		IMAPServer: host, IMAPPort: portNumber, Insecure: true,
		SC: &config.SessionCache{},
	}, disconnected
}
