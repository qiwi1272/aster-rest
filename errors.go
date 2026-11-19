package asterRest

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Every failed request falls in one of three classes. Act on the class, not the message:
//
//   - not sent (errors.Is(err, ErrNotSent)): it never left the process. Safe to retry or drop.
//     ErrBackoff is one: the venue rate-limited this base URL and the hold has not run out.
//   - unknown (IsUnknown): it may or may not have executed (transport failure, 5xx, 408, or codes
//     -1006/-1007). For an order, query it by client id before placing anything in its place.
//   - rejected: the venue answered and did not execute it. IsDefinitiveReject lists the codes
//     where retrying the same request cannot succeed; IsPostOnly, IsNotFound and IsRateLimited
//     name the common cases.
var (
	ErrNotSent = errors.New("aster: request not sent")
	ErrBackoff = fmt.Errorf("%w: rate-limit hold", ErrNotSent)
)

// IsUnknown reports whether err leaves the request's outcome unknown.
func IsUnknown(err error) bool {
	var a *APIError
	return errors.As(err, &a) && a.Unknown
}

// IsDefinitiveReject reports whether the venue refused the request in a way a retry of the same
// request cannot fix (bad parameters, filters, margin, reduce-only, post-only).
func IsDefinitiveReject(err error) bool {
	var a *APIError
	if !errors.As(err, &a) || a.Unknown {
		return false
	}
	switch a.Code {
	case -1100, -1101, -1102, -1111, -1116, -1117, -1121, // malformed request, precision, type, side, symbol
		-2010, -2014, -2015, -2019, -2022, // order rejected, API key, margin, reduce-only
		-4003, -4004, -4005, -4014, -4023, // quantity and price filters
		-5022: // post-only would have taken
		return true
	}
	return false
}

// IsPostOnly reports whether a post-only (GTX) order was refused because it would have taken.
func IsPostOnly(err error) bool {
	var a *APIError
	return errors.As(err, &a) && !a.Unknown && (a.Code == -5022 || a.Code == -2010)
}

// IsNotFound reports whether the order a cancel or query named does not exist (or is no longer open).
func IsNotFound(err error) bool {
	var a *APIError
	return errors.As(err, &a) && !a.Unknown && (a.Code == -2011 || a.Code == -2013)
}

// IsRateLimited reports whether the venue answered 429 (rate limit) or 418 (IP ban). Requests to
// the same base URL then return ErrBackoff until the hold set from Retry-After runs out.
func IsRateLimited(err error) bool {
	var a *APIError
	return errors.As(err, &a) && (a.Status == 429 || a.Status == 418)
}

// Rate-limit holds, per base URL and shared by every Rest in the process: the venue's limits
// are per IP, so one client's 429 holds the others too.
var (
	holdMu sync.Mutex
	holds  = map[string]time.Time{}
)

const (
	minHold = 5 * time.Second
	maxHold = time.Hour
)

func holdErr(base string) error {
	holdMu.Lock()
	until := holds[base]
	holdMu.Unlock()
	if wait := time.Until(until); wait > 0 {
		return fmt.Errorf("%w for another %s", ErrBackoff, wait.Round(time.Millisecond))
	}
	return nil
}

// setHold starts a hold after a 429 or 418, for Retry-After seconds, kept within 5s..1h.
func setHold(base, retryAfter string) {
	secs, _ := strconv.Atoi(strings.TrimSpace(retryAfter))
	d := min(max(time.Duration(secs)*time.Second, minHold), maxHold)
	holdMu.Lock()
	if until := time.Now().Add(d); until.After(holds[base]) {
		holds[base] = until
	}
	holdMu.Unlock()
}

// redact removes the request's authentication values (signature, nonce, user, signer) from a
// venue error message, so errors can be logged as they are.
func redact(msg string, p url.Values) string {
	for _, k := range []string{"signature", "nonce", "user", "signer"} {
		if v := p.Get(k); v != "" {
			msg = strings.ReplaceAll(msg, v, "[redacted]")
		}
	}
	return msg
}
