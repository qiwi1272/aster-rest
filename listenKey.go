package asterRest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// listenKeyTTL is how long a user data stream survives without a keepalive.
//
//	https://asterdex.github.io/aster-api-website/futures-v3/user-data-streams/
//	"User data streams will close after 60 minutes."
//
// PUT /listenKey is the documented "Keepalive User Data Stream" call — there is
// no separate stream-level keepalive. It takes no parameters and returns {}.
const listenKeyTTL = 60 * time.Minute

// Renew twice per lifetime. That is the retry: one failed renewal still leaves
// a second attempt before the key expires, so no backoff logic is needed. The
// docs suggest pinging "about every 60 minutes", which is the expiry itself —
// renewing at the halfway mark costs one extra request an hour and removes the
// race entirely.
const listenKeyRenewEvery = listenKeyTTL / 2

// listenKeyReqTimeout bounds a single renewal so a hung request cannot eat the
// whole window and cost us the retry.
const listenKeyReqTimeout = 10 * time.Second

type newListenKeyResponse struct {
	ListenKey string `json:"listenKey"`
}

func (a *Rest) getListenKey(ctx context.Context) (string, error) {
	resp, err := a.RequestSigned(ctx, "POST", "/listenKey", nil)
	if err != nil {
		return "", err
	}

	key := &newListenKeyResponse{}
	if err := json.Unmarshal(resp, key); err != nil {
		return "", err
	}

	return key.ListenKey, nil
}

// ListenKey holds a user-data-stream key and keeps it alive until ctx is done.
//
// The key itself never changes — renewal only extends its expiry — so the value
// is safe to read concurrently without a lock.
type ListenKey struct {
	client    *Rest
	ListenKey string
}

// NewListenKey fetches a key and starts renewing it.
//
// ctx governs the renewal goroutine's lifetime, so it must outlive the key.
// Passing a request-scoped context here means every renewal fails the moment
// that request's deadline passes, and the stream dies one TTL later.
func NewListenKey(ctx context.Context, cl *Rest) (*ListenKey, error) {
	key, err := cl.getListenKey(ctx)
	if err != nil {
		return nil, err
	}

	listener := &ListenKey{
		client:    cl,
		ListenKey: key,
	}

	go listener.renew(ctx)

	return listener, nil
}

// renew extends the key on an interval until ctx is cancelled.
func (l *ListenKey) renew(ctx context.Context) {
	ticker := time.NewTicker(listenKeyRenewEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := l.update(ctx); err != nil {
				// Not fatal on its own — the next tick still lands inside the
				// TTL. Two consecutive failures mean the stream is about to go
				// silent with no other symptom, which is worth noticing.
				fmt.Fprintf(os.Stderr, "aster: listenKey renew failed: %v\n", err)
			}
		}
	}
}

func (l *ListenKey) update(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, listenKeyReqTimeout)
	defer cancel()

	_, err := l.client.RequestSigned(ctx, "PUT", "/listenKey", nil)
	return err
}

// Close releases the key server-side. Renewal stops with the context passed to
// NewListenKey; this just avoids leaving a key alive until it times out.
func (l *ListenKey) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, listenKeyReqTimeout)
	defer cancel()

	_, err := l.client.RequestSigned(ctx, "DELETE", "/listenKey", nil)
	return err
}
