package asterRest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/shopspring/decimal"
)

// ModifyOrderRequestPerp changes the price and quantity of an open perp LIMIT order in place
// (PUT /fapi/v3/order). Only LIMIT orders can be modified. A modified order loses its queue
// position, even when only the quantity changes. If the venue rejects the new values the
// original order stays as it was.
type ModifyOrderRequestPerp struct {
	market   *Market
	side     OrderSide
	qty      decimal.Decimal
	price    decimal.Decimal
	orderID  uint64
	clientID string
}

// NewModifyOrderPerp builds a modify for an order on m. side must be the order's side: it sets
// the rounding direction (buys round the price down, sells up, as for a new order) and is sent,
// so the venue refuses the modify if it is wrong. Name the order with ByOrderID or ByClientID.
func NewModifyOrderPerp(m *Market, side OrderSide, qty, price decimal.Decimal) *ModifyOrderRequestPerp {
	return &ModifyOrderRequestPerp{market: m, side: side, qty: qty, price: price}
}

// ByOrderID names the order to modify by its venue order id.
func (r *ModifyOrderRequestPerp) ByOrderID(id uint64) *ModifyOrderRequestPerp {
	r.orderID = id
	return r
}

// ByClientID names the order to modify by the client order id it was placed with.
func (r *ModifyOrderRequestPerp) ByClientID(id string) *ModifyOrderRequestPerp {
	r.clientID = id
	return r
}

// Build rounds and checks the new price and quantity exactly as LimitOrderRequestPerp does (lot,
// tick, quantity and price limits, price band), except the minimum notional, which the venue
// documents only for new orders. Exactly one of ByOrderID and ByClientID must have been set.
func (r *ModifyOrderRequestPerp) Build() (url.Values, error) {
	if (r.orderID == 0) == (r.clientID == "") {
		return nil, errors.New("modify needs exactly one of order id and client order id")
	}
	if r.clientID != "" && !clientIDPattern.MatchString(r.clientID) {
		return nil, errors.New("invalid client order id")
	}
	// reduceOnly only skips the notional check here; it is not sent.
	limit := LimitOrderRequestPerp{market: r.market, side: r.side, tiF: TimeInForceGTC, qty: r.qty, price: r.price, reduceOnly: true}
	v, err := limit.Build()
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	if r.orderID != 0 {
		form.Set("orderId", strconv.FormatUint(r.orderID, 10))
	} else {
		form.Set("origClientOrderId", r.clientID)
	}
	form.Set("symbol", v.Get("symbol"))
	form.Set("side", v.Get("side"))
	form.Set("quantity", v.Get("quantity"))
	form.Set("price", v.Get("price"))
	return form, nil
}

// Modify changes an open perp LIMIT order's price and quantity (PUT /order, weight 1). Errors are
// classed as for any request: IsNotFound when the order is gone (filled or canceled first),
// IsDefinitiveReject when the new values fail the filters.
func (a *Rest) Modify(ctx context.Context, req *ModifyOrderRequestPerp) (o OrderResponsePerp, err error) {
	form, err := req.Build()
	if err != nil {
		return o, err
	}
	err = a.SignedJSON(ctx, "PUT", "/order", form, &o)
	return o, err
}

// MaxBatchModify is the venue's batch modify limit for ordinary accounts. Market-maker
// whitelisted accounts may send 10; set it to 10 only if yours is one.
var MaxBatchModify = 5

// BatchModifyResults modifies up to MaxBatchModify orders in one PUT /batchOrders (weight 5).
// The batch is not atomic: each member succeeds or fails alone, a failed member leaves its order
// untouched, and results come back in request order. Inspect every member.
//
// When the venue answers 200 but the reply cannot be matched to the request, the error is an
// unknown-outcome *APIError (IsUnknown): reconcile open orders before acting.
func BatchModifyResults(ctx context.Context, client *Rest, reqs []*ModifyOrderRequestPerp) ([]BatchOrderResult, error) {
	if len(reqs) == 0 || len(reqs) > MaxBatchModify {
		return nil, fmt.Errorf("batch modify size must be 1..%d", MaxBatchModify)
	}
	wire := make([]map[string]string, len(reqs))
	for i, r := range reqs {
		v, err := r.Build()
		if err != nil {
			return nil, fmt.Errorf("batch member %d: %w", i, err)
		}
		wire[i] = make(map[string]string, len(v))
		for k := range v {
			wire[i][k] = v.Get(k)
		}
	}
	b, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	raw, err := client.RequestSigned(ctx, "PUT", "/batchOrders", url.Values{"batchOrders": {string(b)}})
	if err != nil {
		return nil, err
	}
	return batchMembers(raw, len(reqs))
}

// BatchModify is BatchModifyResults with the member failures joined into one error. A nil entry
// in the slice is a member that failed.
func BatchModify(ctx context.Context, client *Rest, reqs []*ModifyOrderRequestPerp) ([]*OrderResponsePerp, error) {
	results, err := BatchModifyResults(ctx, client, reqs)
	if err != nil {
		return nil, err
	}
	out := make([]*OrderResponsePerp, len(results))
	var failures []error
	for i, r := range results {
		out[i] = r.Order
		if r.Error != nil {
			failures = append(failures, fmt.Errorf("batch member %d: %w", i, r.Error))
		}
	}
	return out, errors.Join(failures...)
}

// batchMembers splits a 200 batch reply into per-member results. A reply that does not match
// the request is an unknown outcome: the venue accepted the batch, so members may have executed.
func batchMembers(raw []byte, n int) ([]BatchOrderResult, error) {
	unknown := func(msg string, cause error) error {
		return &APIError{Status: 200, Unknown: true, Message: msg, Cause: cause}
	}
	var members []json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, unknown("unreadable batch reply", err)
	}
	if len(members) != n {
		return nil, unknown(fmt.Sprintf("batch reply has %d members for %d requests", len(members), n), nil)
	}
	out := make([]BatchOrderResult, n)
	for i, m := range members {
		var e APIError
		if err := json.Unmarshal(m, &e); err != nil {
			return nil, unknown(fmt.Sprintf("unreadable batch member %d", i), err)
		}
		if e.Code < 0 {
			e.Status = 200
			e.Unknown = e.Code == -1006 || e.Code == -1007 // the venue lost track of this member
			out[i].Error = &e
			continue
		}
		var o OrderResponsePerp
		if err := json.Unmarshal(m, &o); err != nil {
			return nil, unknown(fmt.Sprintf("unreadable batch member %d", i), err)
		}
		if o.OrderId == 0 {
			return nil, unknown(fmt.Sprintf("batch member %d has no order id", i), nil)
		}
		out[i].Order = &o
	}
	return out, nil
}
