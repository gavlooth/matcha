package fetcher

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/floatpane/matcha/config"
)

// imapPoolMaxIdle is how many unused, logged-in clients are kept per account.
const imapPoolMaxIdle = 2

// imapPoolIdleTimeout is how long an unused client stays open before it is closed.
const imapPoolIdleTimeout = 5 * time.Minute

// imapPoolNoopTimeout is how long a pooled client may take to answer NOOP
// before it is treated as dead; a connection that vanished during suspend or
// behind a NAT would otherwise block the next command for minutes.
const imapPoolNoopTimeout = 5 * time.Second

// imapPool keeps logged-in IMAP clients so repeated searches skip the TLS
// handshake and login.
var imapPool = func() *clientPool[*imapclient.Client] {
	p := newClientPool(connect, imapPoolIdleTimeout)
	p.answers = answersNoop
	return p
}()

// answersNoop reports whether c answers NOOP within imapPoolNoopTimeout.
func answersNoop(c *imapclient.Client) bool {
	timer := time.AfterFunc(imapPoolNoopTimeout, func() { c.Close() }) //nolint:errcheck
	err := c.Noop().Wait()
	return timer.Stop() && err == nil
}

// isServerReply reports whether err is the server answering NO or BAD. The
// connection still works then, so the client is kept and the command is not
// repeated on a new connection.
func isServerReply(err error) bool {
	var reply *imap.Error
	return errors.As(err, &reply)
}

// withIMAPClient runs fn on an authenticated IMAP connection for account.
//
// The client is taken from the pool of unused clients for the account, or a
// new one is dialed. Each caller gets its own client; fn must SELECT the
// folder it needs and must leave the client usable. When fn succeeds the
// client is returned to the pool. When fn fails on a client that came from the
// pool, that client is closed and fn runs once more on a newly dialed client,
// because servers drop connections that sat unused.
func withIMAPClient(account *config.Account, fn func(c *imapclient.Client) error) error {
	return imapPool.run(account, fn)
}

// CloseIMAPPool closes every unused pooled IMAP client. Clients that are in
// use go back to the pool as usual when their caller finishes.
func CloseIMAPPool() {
	imapPool.closeIdle()
}

// clientPool holds unused clients per account. C is *imapclient.Client in
// production; tests use a fake client and a fake dial function.
type clientPool[C io.Closer] struct {
	dial        func(*config.Account) (C, error)
	idleTimeout time.Duration
	// answers, when set, checks that a pooled client still responds before
	// it is reused.
	answers func(C) bool

	mu   sync.Mutex
	idle map[string][]*idleClient[C] // oldest first
}

// idleClient is an unused client, when it was returned, and the timer that
// closes it after the idle timeout. returned is wall-clock time, so time
// spent in suspend counts, which the timer's monotonic clock does not.
type idleClient[C io.Closer] struct {
	client   C
	returned time.Time
	timer    *time.Timer
}

func newClientPool[C io.Closer](dial func(*config.Account) (C, error), idleTimeout time.Duration) *clientPool[C] {
	return &clientPool[C]{
		dial:        dial,
		idleTimeout: idleTimeout,
		idle:        make(map[string][]*idleClient[C]),
	}
}

// poolKey identifies the server login a client belongs to.
func poolKey(account *config.Account) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%s", account.ID, account.GetIMAPServer(), account.GetIMAPPort(), account.Email)
}

// run calls fn with a pooled or newly dialed client; see withIMAPClient.
func (p *clientPool[C]) run(account *config.Account, fn func(C) error) error {
	key := poolKey(account)

	if c, ok := p.take(key); ok {
		if p.answers == nil || p.answers(c) {
			err := fn(c)
			if err == nil || isServerReply(err) {
				p.put(key, c)
				return err
			}
		}
		c.Close() //nolint:errcheck
	}

	c, err := p.dial(account)
	if err != nil {
		return err
	}
	if err := fn(c); err != nil {
		if isServerReply(err) {
			p.put(key, c)
		} else {
			c.Close() //nolint:errcheck
		}
		return err
	}
	p.put(key, c)
	return nil
}

// take removes and returns the most recently returned unused client for key.
// Clients unused for the idle timeout by wall-clock time (for example across
// a suspend, when their timer has not fired yet) are closed instead.
func (p *clientPool[C]) take(key string) (C, bool) {
	var stale []*idleClient[C]
	defer func() {
		for _, entry := range stale {
			entry.client.Close() //nolint:errcheck
		}
	}()
	p.mu.Lock()
	defer p.mu.Unlock()

	for list := p.idle[key]; len(list) > 0; list = p.idle[key] {
		last := len(list) - 1
		entry := list[last]
		p.setIdle(key, slices.Delete(list, last, last+1))
		// If the timer already fired, its callback finds the entry gone and does nothing.
		entry.timer.Stop()
		if time.Now().Round(0).Sub(entry.returned) >= p.idleTimeout {
			stale = append(stale, entry)
			continue
		}
		return entry.client, true
	}
	var zero C
	return zero, false
}

// put returns c to the pool for key. When the account already has the
// maximum number of unused clients, the oldest one is closed.
func (p *clientPool[C]) put(key string, c C) {
	entry := &idleClient[C]{client: c, returned: time.Now().Round(0)}

	p.mu.Lock()
	list := append(p.idle[key], entry)
	var oldest *idleClient[C]
	if len(list) > imapPoolMaxIdle {
		oldest = list[0]
		list = slices.Delete(list, 0, 1)
	}
	p.setIdle(key, list)
	entry.timer = time.AfterFunc(p.idleTimeout, func() { p.closeAfterIdle(key, entry) })
	p.mu.Unlock()

	if oldest != nil {
		oldest.timer.Stop()
		oldest.client.Close() //nolint:errcheck
	}
}

// closeAfterIdle closes entry if it is still unused when its timer fires.
func (p *clientPool[C]) closeAfterIdle(key string, entry *idleClient[C]) {
	p.mu.Lock()
	list := p.idle[key]
	i := slices.Index(list, entry)
	if i >= 0 {
		p.setIdle(key, slices.Delete(list, i, i+1))
	}
	p.mu.Unlock()

	if i >= 0 {
		entry.client.Close() //nolint:errcheck
	}
}

// closeIdle closes every unused client in the pool.
func (p *clientPool[C]) closeIdle() {
	p.mu.Lock()
	all := p.idle
	p.idle = make(map[string][]*idleClient[C])
	p.mu.Unlock()

	for _, list := range all {
		for _, entry := range list {
			entry.timer.Stop()
			entry.client.Close() //nolint:errcheck
		}
	}
}

// setIdle stores list for key, removing the key when the list is empty.
// The caller must hold p.mu.
func (p *clientPool[C]) setIdle(key string, list []*idleClient[C]) {
	if len(list) == 0 {
		delete(p.idle, key)
		return
	}
	p.idle[key] = list
}
