package asterRest

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestErrorClasses(t *testing.T) {
	cases := []struct {
		status                                  int
		body                                    string
		unknown, definitive, postOnly, notFound bool
	}{
		{503, `{"code":-1000,"msg":"x"}`, true, false, false, false},
		{408, ``, true, false, false, false},
		{400, `{"code":-1007,"msg":"Timeout waiting for response from backend server"}`, true, false, false, false},
		{400, `{"code":-5022,"msg":"post only"}`, false, true, true, false},
		{400, `{"code":-4014,"msg":"tick"}`, false, true, false, false},
		{400, `{"code":-2011,"msg":"Unknown order sent."}`, false, false, false, true},
		{400, `{"code":-1021,"msg":"nonce"}`, false, false, false, false},
	}
	for _, c := range cases {
		r := testRest(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		})
		_, err := r.RequestSigned(context.Background(), "POST", "/order", nil)
		if IsUnknown(err) != c.unknown || IsDefinitiveReject(err) != c.definitive || IsPostOnly(err) != c.postOnly || IsNotFound(err) != c.notFound || errors.Is(err, ErrNotSent) {
			t.Errorf("%d %s: %v unknown=%v definitive=%v postOnly=%v notFound=%v", c.status, c.body, err,
				IsUnknown(err), IsDefinitiveReject(err), IsPostOnly(err), IsNotFound(err))
		}
	}
}

func TestRateLimitHoldsEveryClientOnTheBaseURL(t *testing.T) {
	var calls atomic.Int32
	r := testRest(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(429)
		w.Write([]byte(`{"code":-1003,"msg":"too many requests"}`))
	})
	t.Cleanup(func() { holdMu.Lock(); delete(holds, r.Client.BaseURL); holdMu.Unlock() })
	_, err := r.RequestSigned(context.Background(), "POST", "/order", nil)
	if !IsRateLimited(err) || errors.Is(err, ErrNotSent) {
		t.Fatalf("first reply: %v", err)
	}
	// A second client on the same base URL, signed and public, is held without a request.
	r2, _ := NewRest(false, "0x0000000000000000000000000000000000000001", strings.Repeat("0", 63)+"1")
	r2.Client.BaseURL = r.Client.BaseURL
	before := false
	r2.Before = func(context.Context, string, string) error { before = true; return nil }
	_, err = r2.RequestSigned(context.Background(), "POST", "/order", nil)
	if !errors.Is(err, ErrBackoff) || !errors.Is(err, ErrNotSent) || before {
		t.Fatalf("held signed request: %v (Before called %v)", err, before)
	}
	if _, err = r2.ExchangeInfo(context.Background()); !errors.Is(err, ErrBackoff) {
		t.Fatalf("held public request: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("%d requests reached the venue", calls.Load())
	}
	holdMu.Lock()
	left := time.Until(holds[r.Client.BaseURL])
	holdMu.Unlock()
	if left < 6*time.Second || left > 7*time.Second {
		t.Fatalf("hold %v, want Retry-After 7s", left)
	}
}

func TestHoldIsClamped(t *testing.T) {
	base := "test://clamp"
	t.Cleanup(func() { holdMu.Lock(); delete(holds, base); holdMu.Unlock() })
	setHold(base, "")
	holdMu.Lock()
	left := time.Until(holds[base])
	holdMu.Unlock()
	if left < 4*time.Second || left > 5*time.Second {
		t.Fatalf("missing Retry-After: hold %v, want 5s", left)
	}
	setHold(base, "999999")
	holdMu.Lock()
	left = time.Until(holds[base])
	holdMu.Unlock()
	if left > time.Hour {
		t.Fatalf("hold %v over an hour", left)
	}
}

func TestErrorsAreRedacted(t *testing.T) {
	r := testRest(t, func(w http.ResponseWriter, req *http.Request) {
		req.ParseForm()
		w.WriteHeader(400)
		w.Write([]byte(`{"code":-5018,"msg":"bad request signature=` + req.Form.Get("signature") + ` nonce=` + req.Form.Get("nonce") + `"}`))
	})
	_, err := r.RequestSigned(context.Background(), "POST", "/order", nil)
	msg := err.Error()
	if strings.Contains(msg, "0x") || !strings.Contains(msg, "signature=[redacted] nonce=[redacted]") {
		t.Fatalf("not redacted: %s", msg)
	}
}
