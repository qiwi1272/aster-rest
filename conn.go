package asterRest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/qiwi1272/aster-rest/internal/eip712"
)

// Connections
//
// By default every request, read or write, goes through one http.Transport. The venue speaks
// HTTP/2, so that is one TCP connection with the requests multiplexed over it: an order shares its
// TCP stream, and any loss or a large response on it, with whatever reads are in flight.
// RequestResult.Conn and Reused show which connection each request used.
//
// SeparateWrites adds a second connection pool used only by signed POST, PUT and DELETE (orders,
// modifies, cancels), so writes go out in parallel with reads instead of behind them.

// SeparateWrites gives signed writes (POST, PUT, DELETE) their own transport and so their own
// connection; GETs, signed or public, stay on the first. It then opens that connection with one
// GET /ping (weight 1), so the first order does not pay the dial and TLS handshake. An error
// from that ping is returned, but the separation stays on and the next write dials as needed.
//
// The new transport is a copy of Client.Transport as it is when this is called. Like the first
// one, it closes the connection after 90s idle, so a write after a long quiet spell dials again.
// Call it once, before the client is shared between goroutines.
func (a *Rest) SeparateWrites(ctx context.Context) error {
	if t, ok := a.Client.Transport.(*http.Transport); ok && t != nil {
		a.writes = t.Clone()
	} else {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.ForceAttemptHTTP2 = true
		a.writes = t
	}
	if err := holdErr(a.Client.BaseURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.Client.BaseURL+"/ping", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", eip712.USER_AGENT)
	resp, err := (&http.Client{Transport: a.writes, Timeout: a.Client.Timeout}).Do(req)
	if err != nil {
		return fmt.Errorf("warming write connection: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusTeapot {
		setHold(a.Client.BaseURL, resp.Header.Get("Retry-After"))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("warming write connection: status %d", resp.StatusCode)
	}
	return nil
}

// WritesSeparate reports whether SeparateWrites is on.
func (a *Rest) WritesSeparate() bool { return a.writes != nil }

// requestTrace records when a request was written and which connection carried it. HTTP/2
// reports WroteRequest from its own goroutine, hence the lock.
type requestTrace struct {
	mu     sync.Mutex
	wrote  time.Time
	reused bool
	conn   string
}

func (t *requestTrace) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GotConn: func(i httptrace.GotConnInfo) {
			t.mu.Lock()
			t.reused = i.Reused
			if i.Conn != nil {
				t.conn = i.Conn.LocalAddr().String()
			}
			t.mu.Unlock()
		},
		WroteRequest: func(i httptrace.WroteRequestInfo) {
			if i.Err != nil {
				return
			}
			t.mu.Lock()
			t.wrote = time.Now()
			t.mu.Unlock()
		},
	}
}

// fill sets the timing and connection fields of r, whose Started and Elapsed are already set.
func (t *requestTrace) fill(r *RequestResult, sign time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r.Sign = sign
	r.Reused, r.Conn = t.reused, t.conn
	r.Send = r.Elapsed
	if !t.wrote.IsZero() {
		r.Send = min(t.wrote.Sub(r.Started), r.Elapsed)
		r.Ack = r.Elapsed - r.Send
	}
}
