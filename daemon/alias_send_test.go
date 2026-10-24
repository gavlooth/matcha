package daemon

import (
	"bufio"
	"fmt"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/daemonclient"
	"github.com/floatpane/matcha/daemonrpc"
	"github.com/floatpane/matcha/internal/send"
	"github.com/floatpane/matcha/tui"
)

type smtpDelivery struct {
	from    string
	message string
}

func receiveSMTP(t *testing.T) (int, <-chan smtpDelivery) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	deliveries := make(chan smtpDelivery, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := textproto.NewReader(bufio.NewReader(conn))
		writer := textproto.NewWriter(bufio.NewWriter(conn))
		if writer.PrintfLine("220 localhost ESMTP") != nil {
			return
		}
		var from string
		for {
			line, err := reader.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO "), strings.HasPrefix(line, "HELO "):
				_ = writer.PrintfLine("250 localhost")
			case strings.HasPrefix(line, "MAIL FROM:"):
				from = strings.TrimPrefix(line, "MAIL FROM:")
				_ = writer.PrintfLine("250 OK")
			case line == "DATA":
				_ = writer.PrintfLine("354 Send message")
				data, err := reader.ReadDotBytes()
				if err != nil {
					return
				}
				deliveries <- smtpDelivery{from: from, message: string(data)}
				_ = writer.PrintfLine("250 OK")
			case line == "QUIT":
				_ = writer.PrintfLine("221 Bye")
				return
			default:
				_ = writer.PrintfLine("250 OK")
			}
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, deliveries
}

func TestAliasReplyUsesSelectedFromThroughDirectAndQueuedSend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, mode := range []string{"direct", "daemon"} {
		for _, override := range []string{"", "User <alias@example.com>"} {
			t.Run(fmt.Sprintf("%s/%s", mode, override), func(t *testing.T) {
				port, deliveries := receiveSMTP(t)
				cfg := &config.Config{DisableDaemon: true, Accounts: []config.Account{{
					ID: "account", Name: "User", Email: "login@example.com", Password: "secret",
					FetchEmail: "main@example.com", SendAsEmail: "default@example.com",
					ServiceProvider: "custom", SMTPServer: "127.0.0.1", SMTPPort: port,
					IMAPServer: "127.0.0.1", IMAPPort: 1,
					SC: &config.SessionCache{},
				}}}
				if mode == "direct" {
					service := daemonclient.NewService(cfg)
					defer service.Close()
					result := send.SendEmailCmd(&send.Dependencies{Service: service, Config: cfg}, tui.SendEmailMsg{
						AccountID: "account", FromOverride: override, To: "recipient@example.net", Subject: "Alias reply", Body: "Reply body",
					})()
					if failed, ok := result.(tui.EmailResultMsg); ok {
						t.Fatalf("send failed: %v", failed.Err)
					}
				} else {
					d := New(cfg)
					d.sendOutboxEntry(&OutboxEntry{ID: "reply", Params: daemonrpc.SendEmailParams{
						AccountID: "account", FromOverride: override, To: []string{"recipient@example.net"}, Subject: "Alias reply", Body: "Reply body",
					}})
				}
				select {
				case delivered := <-deliveries:
					want := "default@example.com"
					if override != "" {
						want = "alias@example.com"
					}
					if delivered.from != "<"+want+">" {
						t.Errorf("SMTP envelope From = %q, want %s", delivered.from, want)
					}
					message, err := mail.ReadMessage(strings.NewReader(delivered.message))
					if err != nil {
						t.Fatal(err)
					}
					messageID := message.Header.Get("Message-ID")
					if parsed, err := mail.ParseAddress(messageID); err != nil || parsed.Name != "" {
						t.Errorf("SMTP message has invalid Message-ID %q: %v", messageID, err)
					}
					address, err := mail.ParseAddress(message.Header.Get("From"))
					if err != nil {
						t.Fatal(err)
					}
					if address.Address != want {
						t.Errorf("message From = %q, want %s", address.Address, want)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("no SMTP delivery")
				}
				if got := cfg.Accounts[0].SendAsEmail; got != "default@example.com" {
					t.Errorf("reply changed saved sender to %q", got)
				}
			})
		}
	}
}
