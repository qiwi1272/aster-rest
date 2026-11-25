package asterRest

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/qiwi1272/aster-rest/internal/eip712"
	"github.com/shopspring/decimal"
)

// recoverSigner checks the request is signed like every other v3 request: EIP-712 Message over
// the url-encoded parameters without the signature, by the client's signer.
func recoverSigner(t *testing.T, r *Rest, form url.Values) string {
	t.Helper()
	sig := form.Get("signature")
	unsigned := url.Values{}
	for k, v := range form {
		if k != "signature" {
			unsigned[k] = v
		}
	}
	hash, err := eip712.HashEIP712Message(apitypes.TypedDataMessage{"msg": unsigned.Encode()}, "Message", r.Client)
	if err != nil {
		t.Fatal(err)
	}
	b, err := hex.DecodeString(strings.TrimPrefix(sig, "0x"))
	if err != nil || len(b) != 65 {
		t.Fatalf("signature %q", sig)
	}
	b[64] -= 27
	pub, err := crypto.SigToPub(hash, b)
	if err != nil {
		t.Fatal(err)
	}
	return crypto.PubkeyToAddress(*pub).Hex()
}

func TestModifyWireAndSignature(t *testing.T) {
	var got url.Values
	var method, path string
	var rest *Rest
	rest = testRest(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm() // parses PUT bodies
		method, path, got = r.Method, r.URL.Path, r.PostForm
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error(r.Header)
		}
		w.Write([]byte(`{"orderId":405,"symbol":"XAUUSD1","status":"NEW","price":"2000.01","origQty":"0.123","type":"LIMIT","side":"SELL"}`))
	})
	m := testMarket()
	o, err := rest.Modify(context.Background(), NewModifyOrderPerp(m, OrderSideSell, decimal.RequireFromString("0.1239"), decimal.RequireFromString("2000.005")).ByOrderID(405))
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPut || path != "/order" || o.OrderId != 405 || o.Price != "2000.01" {
		t.Fatal(method, path, o)
	}
	want := map[string]string{"symbol": "XAUUSD1", "side": "SELL", "quantity": "0.123", "price": "2000.01", "orderId": "405"}
	for k, v := range want {
		if got.Get(k) != v {
			t.Errorf("%s=%q want %q", k, got.Get(k), v)
		}
	}
	for _, k := range []string{"origClientOrderId", "type", "timeInForce", "reduceOnly"} {
		if got.Has(k) {
			t.Errorf("unexpected %s", k)
		}
	}
	if got.Get("nonce") == "" || got.Get("user") == "" {
		t.Fatal(got)
	}
	if signer := recoverSigner(t, rest, got); signer != rest.signer.Address || got.Get("signer") != signer {
		t.Fatalf("signed by %s, want %s", signer, rest.signer.Address)
	}

	// By client id, buy side rounds down.
	if _, err := rest.Modify(context.Background(), NewModifyOrderPerp(m, OrderSideBuy, decimal.NewFromInt(1), decimal.RequireFromString("2000.009")).ByClientID("mm-1")); err != nil {
		t.Fatal(err)
	}
	if got.Get("origClientOrderId") != "mm-1" || got.Has("orderId") || got.Get("price") != "2000" {
		t.Fatal(got)
	}
}

func TestModifyBuildRejects(t *testing.T) {
	m := testMarket()
	one, px := decimal.NewFromInt(1), decimal.NewFromInt(2000)
	cases := map[string]*ModifyOrderRequestPerp{
		"no id":        NewModifyOrderPerp(m, OrderSideBuy, one, px),
		"both ids":     NewModifyOrderPerp(m, OrderSideBuy, one, px).ByOrderID(1).ByClientID("a"),
		"bad clientID": NewModifyOrderPerp(m, OrderSideBuy, one, px).ByClientID("has space"),
		"no side":      NewModifyOrderPerp(m, "", one, px).ByOrderID(1),
		"below lot":    NewModifyOrderPerp(m, OrderSideBuy, decimal.RequireFromString("0.0009"), px).ByOrderID(1),
		"above max px": NewModifyOrderPerp(m, OrderSideBuy, one, decimal.NewFromInt(200000)).ByOrderID(1),
		"nil market":   NewModifyOrderPerp(nil, OrderSideBuy, one, px).ByOrderID(1),
	}
	for name, c := range cases {
		if _, err := c.Build(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The notional minimum applies to new orders only.
	if _, err := NewModifyOrderPerp(m, OrderSideBuy, decimal.RequireFromString("0.001"), px).ByOrderID(1).Build(); err != nil {
		t.Fatal(err)
	}
	m.MultiplierUp, m.MultiplierDown, m.Mark = decimal.RequireFromString("1.05"), decimal.RequireFromString("0.95"), decimal.NewFromInt(1000)
	if _, err := NewModifyOrderPerp(m, OrderSideBuy, one, px).ByOrderID(1).Build(); err == nil {
		t.Fatal("accepted price outside band")
	}
}

func TestModifyNotFound(t *testing.T) {
	r := testRest(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"code":-2013,"msg":"Order does not exist."}`))
	})
	_, err := r.Modify(context.Background(), NewModifyOrderPerp(testMarket(), OrderSideBuy, decimal.NewFromInt(1), decimal.NewFromInt(2000)).ByOrderID(9))
	if !IsNotFound(err) || IsUnknown(err) {
		t.Fatal(err)
	}
}

func TestBatchModifyPartialAndWire(t *testing.T) {
	r := testRest(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Method != http.MethodPut || r.URL.Path != "/batchOrders" {
			t.Error(r.Method, r.URL.Path)
		}
		var members []map[string]string
		if err := json.Unmarshal([]byte(r.PostForm.Get("batchOrders")), &members); err != nil {
			t.Fatal(err)
		}
		if len(members) != 3 || members[0]["orderId"] != "11" || members[1]["origClientOrderId"] != "c-2" || members[2]["price"] != "2001" {
			t.Error(members)
		}
		w.Write([]byte(`[{"orderId":11,"status":"NEW"},{"code":-2013,"msg":"Order does not exist."},{"code":-1007,"msg":"Timeout"}]`))
	})
	m := testMarket()
	reqs := []*ModifyOrderRequestPerp{
		NewModifyOrderPerp(m, OrderSideBuy, decimal.NewFromInt(1), decimal.NewFromInt(2000)).ByOrderID(11),
		NewModifyOrderPerp(m, OrderSideBuy, decimal.NewFromInt(1), decimal.NewFromInt(2000)).ByClientID("c-2"),
		NewModifyOrderPerp(m, OrderSideSell, decimal.NewFromInt(1), decimal.NewFromInt(2001)).ByOrderID(13),
	}
	res, err := BatchModifyResults(context.Background(), r, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Order == nil || res[0].Order.OrderId != 11 || !IsNotFound(res[1].Error) || !IsUnknown(res[2].Error) {
		t.Fatal(res)
	}
	out, err := BatchModify(context.Background(), r, reqs)
	if err == nil || out[0] == nil || out[1] != nil {
		t.Fatal(out, err)
	}
}

func TestBatchSizeLimitsNeverReachNetwork(t *testing.T) {
	var calls atomic.Int32
	r := testRest(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.Write([]byte(`[]`)) })
	m := testMarket()
	mod := func(n int) []*ModifyOrderRequestPerp {
		out := make([]*ModifyOrderRequestPerp, n)
		for i := range out {
			out[i] = NewModifyOrderPerp(m, OrderSideBuy, decimal.NewFromInt(1), decimal.NewFromInt(2000)).ByOrderID(uint64(i + 1))
		}
		return out
	}
	place := func(n int) []LimitOrderRequest {
		out := make([]LimitOrderRequest, n)
		for i := range out {
			out[i] = NewLimitOrderPerp(m, OrderSideBuy, TimeInForceGTX, decimal.NewFromInt(1), decimal.NewFromInt(2000), false)
		}
		return out
	}
	for _, n := range []int{0, 6} {
		if _, err := BatchModifyResults(context.Background(), r, mod(n)); err == nil {
			t.Errorf("batch modify of %d accepted", n)
		}
		if _, err := BatchOrderLimitResults(context.Background(), r, place(n)); err == nil {
			t.Errorf("batch place of %d accepted", n)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("oversized batch reached the network")
	}
}

func TestBatchModifyMismatchIsUnknown(t *testing.T) {
	r := testRest(t, func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`[{"orderId":1}]`)) })
	m := testMarket()
	reqs := []*ModifyOrderRequestPerp{
		NewModifyOrderPerp(m, OrderSideBuy, decimal.NewFromInt(1), decimal.NewFromInt(2000)).ByOrderID(1),
		NewModifyOrderPerp(m, OrderSideBuy, decimal.NewFromInt(1), decimal.NewFromInt(2000)).ByOrderID(2),
	}
	if _, err := BatchModifyResults(context.Background(), r, reqs); !IsUnknown(err) {
		t.Fatal(err)
	}
}

func TestBatchPlaceAcceptedButUnmatchedIsUnknown(t *testing.T) {
	m := testMarket()
	orders := []LimitOrderRequest{
		NewLimitOrderPerp(m, OrderSideBuy, TimeInForceGTX, decimal.NewFromInt(1), decimal.NewFromInt(2000), false),
		NewLimitOrderPerp(m, OrderSideSell, TimeInForceGTX, decimal.NewFromInt(1), decimal.NewFromInt(2001), false),
	}
	cases := map[string]string{
		"unreadable":     `<html>gateway</html>`,
		"not a list":     `{"orderId":1}`,
		"wrong length":   `[{"orderId":1,"status":"NEW"}]`,
		"missing id":     `[{"orderId":1,"status":"NEW"},{"status":"NEW"}]`,
		"bad member":     `[{"orderId":1,"status":"NEW"},{"orderId":"x"}]`,
		"empty reply":    ``,
		"too many items": `[{"orderId":1},{"orderId":2},{"orderId":3}]`,
	}
	for name, body := range cases {
		r := testRest(t, func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(body)) })
		_, err := BatchOrderLimitResults(context.Background(), r, orders)
		if !IsUnknown(err) || IsDefinitiveReject(err) {
			t.Errorf("%s: %v (IsUnknown=%v)", name, err, IsUnknown(err))
		}
		if _, err := BatchOrderLimit(context.Background(), r, orders); !IsUnknown(err) {
			t.Errorf("%s via BatchOrderLimit: %v", name, err)
		}
	}
}

func TestObserveTiming(t *testing.T) {
	const venue = 30 * time.Millisecond
	r := testRest(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(venue)
		w.Write([]byte(`{}`))
	})
	var results []RequestResult
	r.Observe = func(v RequestResult) { results = append(results, v) }
	if _, err := r.RequestSigned(context.Background(), "PUT", "/order", url.Values{"symbol": {"XAUUSD1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RequestPublic(context.Background(), "/ping", nil); err != nil {
		t.Fatal(err)
	}
	s, p := results[0], results[1]
	if s.Sign <= 0 || s.Send <= 0 || s.Ack < venue || s.Send+s.Ack != s.Elapsed || s.Conn == "" || s.Proto != "HTTP/1.1" {
		t.Fatalf("signed: %+v", s)
	}
	if p.Sign != 0 || p.Ack < venue || !p.Reused || p.Conn != s.Conn {
		t.Fatalf("public: %+v", p)
	}

	// Transport failure before the request is written: all of it is Send.
	r.Client.BaseURL = "http://127.0.0.1:1"
	r.RequestSigned(context.Background(), "POST", "/order", nil)
	if f := results[2]; !f.Unknown || f.Ack != 0 || f.Send != f.Elapsed || f.Sign <= 0 {
		t.Fatalf("failed: %+v", f)
	}
}

// TestConnectionsHTTP2 shows the default (one HTTP/2 connection for everything) and
// SeparateWrites (writes on a second one).
func TestConnectionsHTTP2(t *testing.T) {
	var pings atomic.Int32
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			pings.Add(1)
		}
		w.Write([]byte(`{}`))
	}))
	s.EnableHTTP2 = true
	s.StartTLS()
	t.Cleanup(s.Close)
	newRest := func() *Rest {
		r, err := NewRest(false, "0x0000000000000000000000000000000000000001", strings.Repeat("0", 63)+"1")
		if err != nil {
			t.Fatal(err)
		}
		r.Client.BaseURL = s.URL
		r.Client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: s.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}
		return r
	}
	ctx := context.Background()
	run := func(r *Rest) map[string]RequestResult {
		got := map[string]RequestResult{}
		r.Observe = func(v RequestResult) { got[v.Method] = v }
		for _, m := range []string{"GET", "PUT"} {
			if _, err := r.RequestSigned(ctx, m, "/order", nil); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := r.RequestPublic(ctx, "/ping", nil); err != nil {
			t.Fatal(err)
		}
		return got
	}

	one := run(newRest())
	if one["GET"].Proto != "HTTP/2.0" || one["PUT"].Conn != one["GET"].Conn || !one["PUT"].Reused {
		t.Fatalf("default should share one HTTP/2 connection: %+v", one)
	}

	r := newRest()
	if err := r.SeparateWrites(ctx); err != nil || !r.WritesSeparate() || pings.Load() != 2 {
		t.Fatal(err, pings.Load())
	}
	two := run(r)
	if two["PUT"].Conn == two["GET"].Conn || !two["PUT"].Reused || two["PUT"].Proto != "HTTP/2.0" {
		t.Fatalf("write should use its own, already warm connection: %+v", two)
	}
}
