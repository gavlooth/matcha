package fetcher

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/floatpane/matcha/config"
)

type fakePoolClient struct {
	id     int
	closed atomic.Int32
}

func (c *fakePoolClient) Close() error {
	c.closed.Add(1)
	return nil
}

func (c *fakePoolClient) isClosed() bool { return c.closed.Load() > 0 }

// fakeDialer hands out numbered fake clients and remembers every one it made.
type fakeDialer struct {
	mu      sync.Mutex
	clients []*fakePoolClient
	err     error
}

func (d *fakeDialer) dial(*config.Account) (*fakePoolClient, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return nil, d.err
	}
	c := &fakePoolClient{id: len(d.clients) + 1}
	d.clients = append(d.clients, c)
	return c, nil
}

func (d *fakeDialer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.clients)
}

func (d *fakeDialer) client(i int) *fakePoolClient {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.clients[i]
}

func newTestPool(t *testing.T, idleTimeout time.Duration) (*clientPool[*fakePoolClient], *fakeDialer) {
	t.Helper()
	d := &fakeDialer{}
	p := newClientPool(d.dial, idleTimeout)
	t.Cleanup(p.closeIdle)
	return p, d
}

func testPoolAccount(id string) *config.Account {
	return &config.Account{ID: id, Email: id + "@example.com", ServiceProvider: config.ProviderCustom, IMAPServer: "imap.example.com", IMAPPort: 993}
}

func (p *clientPool[C]) idleCount(account *config.Account) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.idle[poolKey(account)])
}

func TestClientPoolReusesReturnedClient(t *testing.T) {
	p, d := newTestPool(t, time.Hour)
	acc := testPoolAccount("a")

	var used []*fakePoolClient
	for range 3 {
		if err := p.run(acc, func(c *fakePoolClient) error {
			used = append(used, c)
			return nil
		}); err != nil {
			t.Fatalf("run: %v", err)
		}
	}

	if d.count() != 1 {
		t.Fatalf("dialed %d clients, want 1", d.count())
	}
	if used[0] != used[1] || used[1] != used[2] {
		t.Fatalf("runs used clients %d, %d, %d, want the same client", used[0].id, used[1].id, used[2].id)
	}
	if used[0].isClosed() {
		t.Fatal("reused client was closed")
	}
	if p.idleCount(acc) != 1 {
		t.Fatalf("idle clients = %d, want 1", p.idleCount(acc))
	}
}

func TestClientPoolSeparatesAccounts(t *testing.T) {
	p, d := newTestPool(t, time.Hour)
	ok := func(*fakePoolClient) error { return nil }

	if err := p.run(testPoolAccount("a"), ok); err != nil {
		t.Fatal(err)
	}
	if err := p.run(testPoolAccount("b"), ok); err != nil {
		t.Fatal(err)
	}
	if d.count() != 2 {
		t.Fatalf("dialed %d clients, want one per account", d.count())
	}
}

func TestClientPoolKeepsAtMostTwoIdleClients(t *testing.T) {
	p, d := newTestPool(t, time.Hour)
	acc := testPoolAccount("a")

	const callers = 3
	var started sync.WaitGroup
	started.Add(callers)
	release := make(chan struct{})
	got := make([]*fakePoolClient, callers)
	errs := make(chan error, callers)

	var done sync.WaitGroup
	for i := range callers {
		done.Add(1)
		go func() {
			defer done.Done()
			errs <- p.run(acc, func(c *fakePoolClient) error {
				got[i] = c
				started.Done()
				<-release
				return nil
			})
		}()
	}

	started.Wait()
	close(release)
	done.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	}

	if d.count() != callers {
		t.Fatalf("dialed %d clients, want %d (one per concurrent caller)", d.count(), callers)
	}
	if got[0] == got[1] || got[1] == got[2] || got[0] == got[2] {
		t.Fatal("two concurrent callers shared one client")
	}
	if p.idleCount(acc) != imapPoolMaxIdle {
		t.Fatalf("idle clients = %d, want %d", p.idleCount(acc), imapPoolMaxIdle)
	}
	closed := 0
	for i := range callers {
		if d.client(i).isClosed() {
			closed++
		}
	}
	if closed != callers-imapPoolMaxIdle {
		t.Fatalf("closed %d clients, want %d", closed, callers-imapPoolMaxIdle)
	}
}

func TestClientPoolRetriesOnNewClientAfterReusedClientFails(t *testing.T) {
	p, d := newTestPool(t, time.Hour)
	acc := testPoolAccount("a")

	if err := p.run(acc, func(*fakePoolClient) error { return nil }); err != nil {
		t.Fatal(err)
	}
	stale := d.client(0)

	var calls []int
	err := p.run(acc, func(c *fakePoolClient) error {
		calls = append(calls, c.id)
		if c == stale {
			return errors.New("connection reset")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("run after retry: %v", err)
	}
	if len(calls) != 2 || calls[0] != stale.id || calls[1] == stale.id {
		t.Fatalf("fn ran on clients %v, want the pooled client then a new one", calls)
	}
	if !stale.isClosed() {
		t.Fatal("failed pooled client was not closed")
	}
	fresh := d.client(1)
	if fresh.isClosed() || p.idleCount(acc) != 1 {
		t.Fatalf("new client closed=%v idle=%d, want it back in the pool", fresh.isClosed(), p.idleCount(acc))
	}
}

func TestClientPoolDoesNotRetryTwice(t *testing.T) {
	p, d := newTestPool(t, time.Hour)
	acc := testPoolAccount("a")

	if err := p.run(acc, func(*fakePoolClient) error { return nil }); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("mailbox does not exist")
	calls := 0
	err := p.run(acc, func(*fakePoolClient) error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if calls != 2 || d.count() != 2 {
		t.Fatalf("fn calls = %d, dials = %d, want 2 and 2", calls, d.count())
	}
	if !d.client(0).isClosed() || !d.client(1).isClosed() {
		t.Fatal("failed clients were not closed")
	}
	if p.idleCount(acc) != 0 {
		t.Fatalf("idle clients = %d, want 0", p.idleCount(acc))
	}
}

func TestClientPoolNewClientFailureIsNotRetried(t *testing.T) {
	p, d := newTestPool(t, time.Hour)
	acc := testPoolAccount("a")

	wantErr := errors.New("search failed")
	calls := 0
	err := p.run(acc, func(*fakePoolClient) error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) || calls != 1 || d.count() != 1 {
		t.Fatalf("err = %v, calls = %d, dials = %d, want the error after one call on one client", err, calls, d.count())
	}
	if !d.client(0).isClosed() || p.idleCount(acc) != 0 {
		t.Fatal("failed new client was kept")
	}
}

func TestClientPoolReturnsDialError(t *testing.T) {
	p, d := newTestPool(t, time.Hour)
	d.err = errors.New("dial refused")

	called := false
	err := p.run(testPoolAccount("a"), func(*fakePoolClient) error {
		called = true
		return nil
	})
	if !errors.Is(err, d.err) || called {
		t.Fatalf("err = %v, fn called = %v, want dial error and no call", err, called)
	}
}

func TestClientPoolClosesIdleClientAfterTimeout(t *testing.T) {
	p, d := newTestPool(t, 10*time.Millisecond)
	acc := testPoolAccount("a")

	if err := p.run(acc, func(*fakePoolClient) error { return nil }); err != nil {
		t.Fatal(err)
	}
	first := d.client(0)

	deadline := time.Now().Add(2 * time.Second)
	for !first.isClosed() {
		if time.Now().After(deadline) {
			t.Fatal("idle client was not closed after the idle timeout")
		}
		time.Sleep(time.Millisecond)
	}
	if p.idleCount(acc) != 0 {
		t.Fatalf("idle clients = %d after timeout, want 0", p.idleCount(acc))
	}

	if err := p.run(acc, func(*fakePoolClient) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if d.count() != 2 {
		t.Fatalf("dialed %d clients, want a new one after the idle timeout", d.count())
	}
	if first.closed.Load() != 1 {
		t.Fatalf("idle client closed %d times, want 1", first.closed.Load())
	}
}

func TestClientPoolCloseIdle(t *testing.T) {
	p, d := newTestPool(t, time.Hour)
	ok := func(*fakePoolClient) error { return nil }
	if err := p.run(testPoolAccount("a"), ok); err != nil {
		t.Fatal(err)
	}
	if err := p.run(testPoolAccount("b"), ok); err != nil {
		t.Fatal(err)
	}

	p.closeIdle()

	if !d.client(0).isClosed() || !d.client(1).isClosed() {
		t.Fatal("closeIdle left a client open")
	}
	if p.idleCount(testPoolAccount("a")) != 0 || p.idleCount(testPoolAccount("b")) != 0 {
		t.Fatal("closeIdle left clients in the pool")
	}
}

func TestClientPoolKeepsClientWhenServerAnswersNo(t *testing.T) {
	p, d := newTestPool(t, time.Minute)
	account := testPoolAccount("a")
	if err := p.run(account, func(*fakePoolClient) error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	no := &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Mailbox does not exist"}
	err := p.run(account, func(*fakePoolClient) error { calls++; return no })
	if !errors.Is(err, no) || calls != 1 {
		t.Fatalf("err %v after %d calls; a NO reply must be returned without a second try", err, calls)
	}
	if d.count() != 1 || d.client(0).isClosed() || p.idleCount(account) != 1 {
		t.Fatalf("dialed %d, first closed %t, idle %d; the working client must stay in the pool", d.count(), d.client(0).isClosed(), p.idleCount(account))
	}
}

func TestClientPoolDialsAgainWhenPooledClientDoesNotAnswer(t *testing.T) {
	p, d := newTestPool(t, time.Minute)
	p.answers = func(c *fakePoolClient) bool { return c.id != 1 }
	account := testPoolAccount("a")
	p.run(account, func(*fakePoolClient) error { return nil }) //nolint:errcheck
	var used int
	if err := p.run(account, func(c *fakePoolClient) error { used = c.id; return nil }); err != nil {
		t.Fatal(err)
	}
	if used != 2 || !d.client(0).isClosed() {
		t.Fatalf("used client %d, first closed %t; a client that does not answer NOOP must be replaced", used, d.client(0).isClosed())
	}
}

func TestClientPoolDropsClientIdleTooLongByWallClock(t *testing.T) {
	p, d := newTestPool(t, time.Minute)
	account := testPoolAccount("a")
	p.run(account, func(*fakePoolClient) error { return nil }) //nolint:errcheck
	// As after a suspend: the timer has not fired, but the wall clock moved on.
	p.mu.Lock()
	p.idle[poolKey(account)][0].returned = time.Now().Add(-2 * time.Minute)
	p.mu.Unlock()
	var used int
	p.run(account, func(c *fakePoolClient) error { used = c.id; return nil }) //nolint:errcheck
	if used != 2 || !d.client(0).isClosed() {
		t.Fatalf("used client %d, first closed %t; a client unused past the timeout must not be reused", used, d.client(0).isClosed())
	}
}
