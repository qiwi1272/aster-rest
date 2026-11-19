package asterRest

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/shopspring/decimal"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func testMarket() *Market {
	return &Market{Upper: "XAUUSD1", Status: "TRADING", Step: decimal.RequireFromString("0.01"), Lot: decimal.RequireFromString("0.001"), MinQL: decimal.RequireFromString("0.001"), MaxQL: decimal.NewFromInt(10000), MinP: decimal.RequireFromString("0.01"), MaxP: decimal.NewFromInt(100000), MinNotional: decimal.NewFromInt(5)}
}
func testRest(t *testing.T, handler http.HandlerFunc) *Rest {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	r, e := NewRest(false, "0x0000000000000000000000000000000000000001", strings.Repeat("0", 63)+"1")
	if e != nil {
		t.Fatal(e)
	}
	r.Client.BaseURL = s.URL
	return r
}
func TestRequestConcurrentNonceAndCallerOwnership(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	r := testRest(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		n := r.Form.Get("nonce")
		if _, e := strconv.ParseInt(n, 10, 64); e != nil {
			t.Error(e)
		}
		mu.Lock()
		if seen[n] {
			t.Errorf("duplicate nonce %s", n)
		}
		seen[n] = true
		mu.Unlock()
		if r.Form.Get("signature") == "" {
			t.Error("missing signature")
		}
		w.Write([]byte(`{}`))
	})
	params := url.Values{"symbol": {"XAUUSD1"}}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := r.RequestSigned(context.Background(), "GET", "/order", params); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if len(params) != 1 {
		t.Fatal("caller params mutated")
	}
	if len(seen) != 100 {
		t.Fatal(len(seen))
	}
}
func TestBatchScalarJSONAndPartialSuccess(t *testing.T) {
	r := testRest(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var orders []map[string]string
		if e := json.Unmarshal([]byte(r.Form.Get("batchOrders")), &orders); e != nil {
			t.Fatal(e)
		}
		if len(orders) != 2 || orders[0]["newClientOrderId"] != "test-1" {
			t.Error(orders)
		}
		w.Write([]byte(`[{"orderId":1,"status":"NEW"},{"code":-5022,"msg":"post only"}]`))
	})
	m := testMarket()
	orders := []LimitOrderRequest{NewLimitOrderPerp(m, OrderSideBuy, TimeInForceGTX, decimal.NewFromInt(1), decimal.NewFromInt(2000), false).WithClientID("test-1"), NewLimitOrderPerp(m, OrderSideSell, TimeInForceGTX, decimal.NewFromInt(1), decimal.NewFromInt(2001), false)}
	v, e := BatchOrderLimitResults(context.Background(), r, orders)
	if e != nil {
		t.Fatal(e)
	}
	if v[0].Order == nil || v[1].Error == nil || v[1].Order != nil {
		t.Fatal(v)
	}
}
func TestTimeoutStatusAndHeaders(t *testing.T) {
	r := testRest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-MBX-USED-WEIGHT-1M", "77")
		w.WriteHeader(503)
		w.Write([]byte(`{"code":-1000,"msg":"unknown"}`))
	})
	var result RequestResult
	r.Observe = func(v RequestResult) { result = v }
	_, e := r.RequestSigned(context.Background(), "POST", "/order", nil)
	var api *APIError
	if !errors.As(e, &api) || !api.Unknown || result.Header.Get("X-MBX-USED-WEIGHT-1M") != "77" || result.Elapsed <= 0 {
		t.Fatalf("%+v %+v", e, result)
	}
}
func TestOrderFilters(t *testing.T) {
	m := testMarket()
	for _, side := range []OrderSide{OrderSideBuy, OrderSideSell} {
		o := NewLimitOrderPerp(m, side, TimeInForceGTX, decimal.RequireFromString("0.1239"), decimal.RequireFromString("2000.005"), false)
		v, e := o.Build()
		if e != nil {
			t.Fatal(e)
		}
		want := "2000"
		if side == OrderSideSell {
			want = "2000.01"
		}
		if v.Get("price") != want || v.Get("quantity") != "0.123" {
			t.Fatal(v)
		}
	}
	m.Status = "HALT"
	if _, e := NewLimitOrderPerp(m, OrderSideBuy, TimeInForceGTX, decimal.NewFromInt(1), decimal.NewFromInt(2000), false).Build(); e == nil {
		t.Fatal("accepted halted market")
	}
	m.Status = "TRADING"
	if _, e := NewLimitOrderPerp(m, OrderSideBuy, TimeInForceGTX, decimal.RequireFromString("0.001"), decimal.NewFromInt(2000), false).Build(); e == nil {
		t.Fatal("accepted below notional")
	}
	m.MultiplierUp = decimal.RequireFromString("1.05")
	m.MultiplierDown = decimal.RequireFromString(".95")
	if _, e := NewLimitOrderPerp(m, OrderSideBuy, TimeInForceGTX, decimal.NewFromInt(1), decimal.NewFromInt(2000), false).Build(); e == nil {
		t.Fatal("accepted missing mark")
	}
}
func TestMetadataNotionalWireField(t *testing.T) {
	var s Symbol
	if e := json.Unmarshal([]byte(`{"symbol":"XAUUSD1","status":"TRADING","filters":[{"filterType":"MIN_NOTIONAL","notional":"5"}]}`), &s); e != nil {
		t.Fatal(e)
	}
	m, e := s.MarketFromSymbol()
	if e != nil || !m.MinNotional.Equal(decimal.NewFromInt(5)) {
		t.Fatal(m, e)
	}
}

func TestCountdownAndClientLookupWire(t *testing.T) {
	var paths []string
	r := testRest(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		// net/http only parses form bodies automatically for POST/PUT/PATCH.
		if r.Method == http.MethodDelete {
			b, e := io.ReadAll(r.Body)
			if e != nil {
				t.Error(e)
			}
			r.Form, e = url.ParseQuery(string(b))
			if e != nil {
				t.Error(e)
			}
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/countdownCancelAll":
			if r.Form.Get("countdownTime") != "5000" || r.Form.Get("symbol") != "XAUUSD1" {
				t.Error(r.Form)
			}
			w.Write([]byte(`{"countdownTime":"5000"}`))
		default:
			if r.Form.Get("origClientOrderId") != "test-client" {
				t.Error(r.Form)
			}
			w.Write([]byte(`{"orderId":42,"status":"CANCELED"}`))
		}
	})
	if e := r.Countdown(context.Background(), "XAUUSD1", 5*time.Second); e != nil {
		t.Fatal(e)
	}
	if _, e := r.QueryClientOrder(context.Background(), "XAUUSD1", "test-client"); e != nil {
		t.Fatal(e)
	}
	if _, e := r.CancelClientOrder(context.Background(), "XAUUSD1", "test-client"); e != nil {
		t.Fatal(e)
	}
	if len(paths) != 3 {
		t.Fatal(paths)
	}
}
func TestRequestGuardPreventsNetworkDispatch(t *testing.T) {
	called := false
	r := testRest(t, func(w http.ResponseWriter, r *http.Request) { called = true; w.Write([]byte(`{}`)) })
	guard := errors.New("stale intent")
	r.Before = func(context.Context, string, string) error { return guard }
	if _, e := r.RequestSigned(context.Background(), "POST", "/order", nil); !errors.Is(e, guard) {
		t.Fatal(e)
	}
	if called {
		t.Fatal("rejected intent reached network")
	}
}
func TestCancelBatchPartialFailure(t *testing.T) {
	r := testRest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"orderId":1,"status":"CANCELED"},{"code":-2011,"msg":"unknown order"}]`))
	})
	out, e := BatchOrderCancel[OrderCancelResponsePerp](context.Background(), r, testMarket(), []uint64{1, 2})
	if e == nil || out[0] == nil || out[1] != nil {
		t.Fatal(out, e)
	}
}
