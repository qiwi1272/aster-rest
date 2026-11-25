package asterRest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type RequestResult struct {
	Method, Path string
	Started      time.Time
	Elapsed      time.Duration
	Status       int
	Header       http.Header
	Unknown      bool

	// Sign is the time spent building and EIP-712 signing the request; zero for public requests.
	// Send runs from Started until the request is fully written, so it includes waiting for a
	// connection, and any dial and TLS handshake. Ack runs from then until the response body is
	// read: the venue's turnaround plus the network. Send+Ack is Elapsed; Sign comes before Started.
	// If the request failed before it was fully written, Send is all of Elapsed and Ack is zero.
	Sign, Send, Ack time.Duration
	// The connection the request went out on: Reused is false when it was dialed for this request;
	// Conn is its local address, so results with the same Conn shared one connection.
	Reused bool
	Conn   string
	Proto  string // "HTTP/2.0" or "HTTP/1.1"; empty if no response arrived
}
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	Status  int    `json:"-"`
	Unknown bool   `json:"-"`
	Cause   error  `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("aster status=%d code=%d unknown=%t: %s", e.Status, e.Code, e.Unknown, e.Message)
}
func (e *APIError) Unwrap() error { return e.Cause }
func (a *Rest) SignedJSON(ctx context.Context, method, path string, p url.Values, out any) error {
	b, e := a.RequestSigned(ctx, method, path, p)
	if e != nil {
		return e
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}
func (a *Rest) QueryClientOrder(ctx context.Context, symbol, id string) (o OrderResponsePerp, e error) {
	e = a.SignedJSON(ctx, "GET", "/order", url.Values{"symbol": {symbol}, "origClientOrderId": {id}}, &o)
	return
}
func (a *Rest) CancelClientOrder(ctx context.Context, symbol, id string) (o OrderResponsePerp, e error) {
	e = a.SignedJSON(ctx, "DELETE", "/order", url.Values{"symbol": {symbol}, "origClientOrderId": {id}}, &o)
	return
}
func (a *Rest) CancelAll(ctx context.Context, symbol string) error {
	return a.SignedJSON(ctx, "DELETE", "/allOpenOrders", url.Values{"symbol": {symbol}}, nil)
}
func (a *Rest) Countdown(ctx context.Context, symbol string, d time.Duration) error {
	return a.SignedJSON(ctx, "POST", "/countdownCancelAll", url.Values{"symbol": {symbol}, "countdownTime": {strconv.FormatInt(d.Milliseconds(), 10)}}, nil)
}

type Commission struct {
	Maker string `json:"makerCommissionRate"`
	Taker string `json:"takerCommissionRate"`
}

func (a *Rest) Commission(ctx context.Context, symbol string) (o Commission, e error) {
	e = a.SignedJSON(ctx, "GET", "/commissionRate", url.Values{"symbol": {symbol}}, &o)
	return
}

type Position struct {
	Symbol     string `json:"symbol"`
	Amount     string `json:"positionAmt"`
	Entry      string `json:"entryPrice"`
	Mark       string `json:"markPrice"`
	Unrealized string `json:"unRealizedProfit"`
	Side       string `json:"positionSide"`
}

func (a *Rest) Positions(ctx context.Context) (o []Position, e error) {
	e = a.SignedJSON(ctx, "GET", "/positionRisk", nil, &o)
	return
}

type AccountSnapshot struct {
	Wallet      string `json:"totalWalletBalance"`
	Unrealized  string `json:"totalUnrealizedProfit"`
	Margin      string `json:"totalMarginBalance"`
	Maintenance string `json:"totalMaintMargin"`
	Available   string `json:"availableBalance"`
}

func (a *Rest) AccountSnapshot(ctx context.Context) (o AccountSnapshot, e error) {
	e = a.SignedJSON(ctx, "GET", "/account", nil, &o)
	return
}
func (a *Rest) OneWay(ctx context.Context) (bool, error) {
	var o struct {
		Dual bool `json:"dualSidePosition"`
	}
	e := a.SignedJSON(ctx, "GET", "/positionSide/dual", nil, &o)
	return !o.Dual, e
}

type Trade struct {
	ID              int64  `json:"id"`
	OrderID         int64  `json:"orderId"`
	Symbol          string `json:"symbol"`
	Side            string `json:"side"`
	Qty             string `json:"qty"`
	Price           string `json:"price"`
	Commission      string `json:"commission"`
	CommissionAsset string `json:"commissionAsset"`
	Realized        string `json:"realizedPnl"`
	Time            int64  `json:"time"`
	Maker           bool   `json:"maker"`
}

func (a *Rest) Trades(ctx context.Context, symbol string, fromID int64) (o []Trade, e error) {
	p := url.Values{"symbol": {symbol}, "limit": {"1000"}}
	if fromID >= 0 {
		p.Set("fromId", strconv.FormatInt(fromID, 10))
	}
	e = a.SignedJSON(ctx, "GET", "/userTrades", p, &o)
	return
}

type Income struct {
	Symbol string `json:"symbol"`
	Kind   string `json:"incomeType"`
	Income string `json:"income"`
	Asset  string `json:"asset"`
	Time   int64  `json:"time"`
	ID     int64  `json:"tranId"`
}

func (a *Rest) Income(ctx context.Context, start int64, page int) (o []Income, e error) {
	e = a.SignedJSON(ctx, "GET", "/income", url.Values{"startTime": {strconv.FormatInt(start, 10)}, "limit": {"1000"}, "page": {strconv.Itoa(page)}}, &o)
	return
}
