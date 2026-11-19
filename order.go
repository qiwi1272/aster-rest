package asterRest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"

	"github.com/shopspring/decimal"
)

// type Order struct {
// Symbol    string      `json:"symbol"`
// Side      OrderSide   `json:"side"`
// Type      OrderType   `json:"type"`
// TiF       TimeInForce `json:"timeInForce,omitempty"`
// Quantity  string      `json:"quantity,omitempty"`
// QuoteQty  string      `json:"quoteOrderQty,omitempty"`
// Price     string      `json:"price,omitempty"`
// Cloid     string      `json:"newClientOrderId,omitempty"`
// StopPrice string      `json:"stopPrice,omitempty"`
// }

type LimitOrderRequestSpot struct {
	market *Market
	side   OrderSide
	tiF    TimeInForce
	qty    decimal.Decimal
	price  decimal.Decimal
}

func (l *LimitOrderRequestSpot) Build() (url.Values, error) {
	form := url.Values{}
	form.Set("symbol", l.market.Upper)
	form.Set("side", string(l.side))
	form.Set("type", string(OrderTypeLimit))
	form.Set("timeInForce", string(l.tiF))

	quantity := l.qty.Sub(l.qty.Mod(l.market.Lot))
	if l.market.MinQL.Cmp(quantity) > 0 {
		return nil, errors.New("Quantity less than min limit")
	}
	if l.market.MaxQL.Cmp(quantity) < 0 {
		return nil, errors.New("Quantity more than max limit")
	}
	form.Set("quantity", quantity.String())

	var roundedPrice decimal.Decimal
	rem := l.price.Mod(l.market.Step)
	if l.side == OrderSideBuy {
		roundedPrice = l.price.Sub(rem)
	} else if !rem.IsZero() {
		roundedPrice = l.price.Add(l.market.Step.Sub(rem))
	} else {
		roundedPrice = l.price
	}
	form.Set("price", roundedPrice.String())

	return form, nil
}

type LimitOrderRequestPerp struct {
	market     *Market
	side       OrderSide
	tiF        TimeInForce
	qty        decimal.Decimal
	price      decimal.Decimal
	reduceOnly bool
	clientID   string
}

func (l *LimitOrderRequestPerp) WithClientID(id string) *LimitOrderRequestPerp {
	l.clientID = id
	return l
}

var clientIDPattern = regexp.MustCompile(`^[.A-Za-z0-9_:/-]{1,36}$`)

func (l *LimitOrderRequestPerp) Build() (url.Values, error) {
	if l.market == nil || !l.market.Lot.IsPositive() || !l.market.Step.IsPositive() || !l.qty.IsPositive() || !l.price.IsPositive() {
		return nil, errors.New("invalid market, quantity or price")
	}
	if l.market.Status != "" && l.market.Status != "TRADING" {
		return nil, errors.New("market is not trading")
	}
	if l.side != OrderSideBuy && l.side != OrderSideSell {
		return nil, errors.New("invalid order side")
	}
	if l.tiF != TimeInForceGTX && l.tiF != TimeInForceGTC && l.tiF != TimeInForceIOC && l.tiF != TimeInForceFOK {
		return nil, errors.New("invalid time in force")
	}
	if l.clientID != "" && !clientIDPattern.MatchString(l.clientID) {
		return nil, errors.New("invalid client order id")
	}
	form := url.Values{}
	if l.clientID != "" {
		form.Set("newClientOrderId", l.clientID)
	}
	form.Set("symbol", l.market.Upper)
	form.Set("side", string(l.side))
	form.Set("type", string(OrderTypeLimit))
	form.Set("timeInForce", string(l.tiF))

	if l.reduceOnly {
		form.Set("reduceOnly", "true")
	}

	quantity := l.qty.Sub(l.qty.Mod(l.market.Lot))
	if l.market.MinQL.Cmp(quantity) > 0 {
		return nil, errors.New("Quantity less than min limit")
	}
	if l.market.MaxQL.Cmp(quantity) < 0 {
		return nil, errors.New("Quantity more than max limit")
	}
	form.Set("quantity", quantity.String())

	var roundedPrice decimal.Decimal
	rem := l.price.Mod(l.market.Step)
	if l.side == OrderSideBuy {
		roundedPrice = l.price.Sub(rem)
	} else if !rem.IsZero() {
		roundedPrice = l.price.Add(l.market.Step.Sub(rem))
	} else {
		roundedPrice = l.price
	}
	if roundedPrice.LessThan(l.market.MinP) || (l.market.MaxP.IsPositive() && roundedPrice.GreaterThan(l.market.MaxP)) {
		return nil, errors.New("price outside filters")
	}
	if !l.reduceOnly && quantity.Mul(roundedPrice).LessThan(l.market.MinNotional) {
		return nil, errors.New("notional below minimum")
	}
	if l.market.MultiplierUp.IsPositive() {
		if !l.market.Mark.IsPositive() {
			return nil, errors.New("mark required for price bands")
		}
		if l.side == OrderSideBuy && roundedPrice.GreaterThan(l.market.Mark.Mul(l.market.MultiplierUp)) {
			return nil, errors.New("buy outside price band")
		}
		if l.side == OrderSideSell && roundedPrice.LessThan(l.market.Mark.Mul(l.market.MultiplierDown)) {
			return nil, errors.New("sell outside price band")
		}
	}
	form.Set("price", roundedPrice.String())

	return form, nil
}

// NewLimitOrderSpot builds a spot limit order request. The fields stay
// unexported so Build() remains the only path to the wire format — that is what
// guarantees the tick/lot rounding and filter checks cannot be bypassed.
func NewLimitOrderSpot(
	m *Market,
	side OrderSide,
	tif TimeInForce,
	qty, price decimal.Decimal,
) *LimitOrderRequestSpot {
	return &LimitOrderRequestSpot{market: m, side: side, tiF: tif, qty: qty, price: price}
}

// NewLimitOrderPerp builds a perp limit order request.
func NewLimitOrderPerp(
	m *Market,
	side OrderSide,
	tif TimeInForce,
	qty, price decimal.Decimal,
	reduceOnly bool,
) *LimitOrderRequestPerp {
	return &LimitOrderRequestPerp{
		market: m, side: side, tiF: tif, qty: qty, price: price, reduceOnly: reduceOnly,
	}
}

type LimitOrderRequest interface {
	Build() (url.Values, error)
}

type OrderResponseSpot struct {
	Symbol      string      `json:"symbol"`
	OrderId     uint64      `json:"orderId"`
	Cloid       string      `json:"clientOrderId"`
	UpdateTime  uint64      `json:"updateTime"`
	Price       string      `json:"price"`
	AvgPrice    string      `json:"avgPrice"`
	OrigQty     string      `json:"origQty"`
	CumQty      string      `json:"cumQty"`
	ExecutedQty string      `json:"executedQty"`
	CumQuote    string      `json:"cumQuote"`
	Status      OrderStatus `json:"status"`
	TiF         TimeInForce `json:"timeInForce"`
	StopPrice   string      `json:"stopPrice"`
	OrigType    OrderType   `json:"origType"`
	Type        OrderType   `json:"type"`
	Side        OrderSide   `json:"side"`
}

type OrderResponsePerp struct {
	OrderResponseSpot
	ReduceOnly    bool   `json:"reduceOnly"`
	PositionSide  string `json:"positionSide"`
	ClosePosition bool   `json:"closePosition,omitempty"`
	ActivatePrice string `json:"activatePrice,omitempty"`
	PriceRate     string `json:"priceRate,omitempty"`
	WorkingType   string `json:"workingType"`
	PriceProtect  bool   `json:"priceProtect,omitempty"`
}

type OrderResponse interface {
	OrderResponseSpot | OrderResponsePerp
}

func (a *Rest) OrderLimit(
	ctx context.Context,
	limit LimitOrderRequest,
	out any,
) error {
	form, err := limit.Build()
	if err != nil {
		return err
	}

	resp, err := a.RequestSigned(ctx, "POST", "/order", form)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(resp, out); err != nil {
		return err
	}

	return nil
}

func OrderLimit[RespType OrderResponse](
	ctx context.Context,
	client *Rest,
	limit LimitOrderRequest,
) (*RespType, error) {
	form, err := limit.Build()
	if err != nil {
		return nil, err
	}

	resp, err := client.RequestSigned(ctx, "POST", "/order", form)
	if err != nil {
		return nil, err
	}

	out := new(RespType)
	if err := json.Unmarshal(resp, out); err != nil {
		return nil, err
	}

	return out, nil
}

// BatchOrderResult preserves partial success; callers must inspect every member.
type BatchOrderResult struct {
	Order *OrderResponsePerp
	Error *APIError
}

func BatchOrderLimitResults(ctx context.Context, client *Rest, orders []LimitOrderRequest) ([]BatchOrderResult, error) {
	if len(orders) == 0 || len(orders) > 5 {
		return nil, errors.New("batch size must be 1..5")
	}
	wire := make([]map[string]string, len(orders))
	for i, o := range orders {
		v, err := o.Build()
		if err != nil {
			return nil, err
		}
		wire[i] = map[string]string{}
		for k := range v {
			wire[i][k] = v.Get(k)
		}
	}
	b, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	raw, err := client.RequestSigned(ctx, "POST", "/batchOrders", url.Values{"batchOrders": {string(b)}})
	if err != nil {
		return nil, err
	}
	var members []json.RawMessage
	if err = json.Unmarshal(raw, &members); err != nil {
		return nil, err
	}
	if len(members) != len(orders) {
		return nil, errors.New("batch response length mismatch; outcome unknown")
	}
	out := make([]BatchOrderResult, len(members))
	for i, r := range members {
		var e APIError
		if err = json.Unmarshal(r, &e); err != nil {
			return nil, err
		}
		if e.Code < 0 {
			out[i].Error = &e
			continue
		}
		var o OrderResponsePerp
		if err = json.Unmarshal(r, &o); err != nil {
			return nil, err
		}
		if o.OrderId == 0 {
			return nil, errors.New("missing batch order id; outcome unknown")
		}
		out[i].Order = &o
	}
	return out, nil
}
func BatchOrderLimit(ctx context.Context, client *Rest, orders []LimitOrderRequest) ([]*OrderResponsePerp, error) {
	results, err := BatchOrderLimitResults(ctx, client, orders)
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

// type OrderCancel struct {
// 	Symbol  string `json:"symbol"`
// 	OrderId uint64 `json:"orderId,omitempty"`
// 	Cloid   string `json:"clientOrderId,omitempty"`
// }

type OrderCancelResponseSpot struct {
	OrderResponseSpot
}

type OrderCancelResponsePerp struct {
	OrderResponsePerp
}

type OrderCancelResponse interface {
	OrderCancelResponseSpot | OrderCancelResponsePerp
}

func (a *Rest) OrderCancel(
	ctx context.Context,
	market *Market,
	id string,
	out any,
) error {
	form := url.Values{}
	form.Set("symbol", market.Upper)
	form.Set("orderId", id)

	resp, err := a.RequestSigned(ctx, "DELETE", "/order", form)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(resp, out); err != nil {
		return err
	}

	return nil
}

func OrderCancel[RespType OrderCancelResponse](
	ctx context.Context,
	client *Rest,
	market *Market,
	id string,
) (*RespType, error) {
	form := url.Values{}
	form.Set("symbol", market.Upper)
	form.Set("orderId", id)

	resp, err := client.RequestSigned(ctx, "DELETE", "/order", form)
	if err != nil {
		return nil, err
	}

	out := new(RespType)
	if err := json.Unmarshal(resp, out); err != nil {
		return nil, err
	}

	return out, nil
}

// type OrderQuery struct {
// 	OrderCancel
// }

type OrderQueryResponseSpot struct {
	OrderResponseSpot
	Time uint64 `json:"time"`
}

type OrderQueryResponsePerp struct {
	OrderResponsePerp
	Time uint64 `json:"time"`
}

type OrderQueryResponse interface {
	OrderQueryResponseSpot | OrderQueryResponsePerp
}

func (a *Rest) GetOrderInfo(
	ctx context.Context,
	market *Market,
	id string,
	out any,
) error {
	form := url.Values{}
	form.Set("symbol", market.Upper)
	form.Set("orderId", id)

	resp, err := a.RequestSigned(ctx, "GET", "/order", form)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(resp, out); err != nil {
		return err
	}

	return nil
}

func GetOrderInfo[RespType OrderQueryResponse](
	ctx context.Context,
	client *Rest,
	market *Market,
	id string,
) (*RespType, error) {
	form := url.Values{}
	form.Set("symbol", market.Upper)
	form.Set("orderId", id)

	resp, err := client.RequestSigned(ctx, "GET", "/order", form)
	if err != nil {
		return nil, err
	}

	out := new(RespType)
	if err := json.Unmarshal(resp, out); err != nil {
		return nil, err
	}

	return out, nil
}

// type OrdersOpen struct {
// 	Symbol string `json:"symbol"`
// }

func (a *Rest) GetOpenOrders(
	ctx context.Context,
	market *Market,
	out any,
) error {
	form := url.Values{}
	form.Set("symbol", market.Upper)

	resp, err := a.RequestSigned(ctx, "GET", "/openOrders", form)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(resp, out); err != nil {
		return err
	}

	return nil
}

func GetOpenOrders[RespType OrderResponse](
	ctx context.Context,
	client *Rest,
	market *Market,
) ([]*RespType, error) {
	form := url.Values{}
	form.Set("symbol", market.Upper)

	resp, err := client.RequestSigned(ctx, "GET", "/openOrders", form)
	if err != nil {
		return nil, err
	}

	out := make([]*RespType, 0)
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, err
	}

	return out, nil
}

// type OrdersCancelAll struct {
// 	Symbol   string   `json:"symbol"`
// 	OrderIds []uint64 `json:"orderIdList,omitempty"`
// 	Cloids   []string `json:"clientOrderIdList,omitempty"`
// }

func (a *Rest) OrderCancelAll(
	ctx context.Context,
	market *Market,
	out any,
) error {
	form := url.Values{}
	form.Set("symbol", market.Upper)

	resp, err := a.RequestSigned(ctx, "DELETE", "/allOpenOrders", form)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(resp, out); err != nil {
		return err
	}

	return nil
}

// OrderCancelAll is retained for compatibility. The venue returns an acknowledgment,
// not a list of canceled orders; use Rest.CancelAll and reconcile open orders.
func OrderCancelAll[RespType OrderCancelResponse](ctx context.Context, client *Rest, market *Market) ([]*RespType, error) {
	return nil, client.CancelAll(ctx, market.Upper)
}

type OrdersCancelAllResponse struct {
	Code uint64 `json:"code"`
	Msg  string `json:"msg"`
}

func BatchOrderCancel[RespType OrderCancelResponse](
	ctx context.Context,
	client *Rest,
	market *Market,
	orderIdList []uint64,
) ([]*RespType, error) {
	sz := len(orderIdList)
	if sz > 10 {
		return nil, errors.New("max cancel batch size is 10")
	}

	batchCancel, err := json.Marshal(orderIdList)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("symbol", market.Upper)
	form.Set("orderIdList", string(batchCancel))

	resp, err := client.RequestSigned(ctx, "DELETE", "/batchOrders", form)
	if err != nil {
		return nil, err
	}

	var members []json.RawMessage
	if err := json.Unmarshal(resp, &members); err != nil {
		return nil, err
	}
	if len(members) != sz {
		return nil, errors.New("cancel batch response length mismatch; outcome unknown")
	}
	out := make([]*RespType, sz)
	var failures []error
	for i, b := range members {
		var api APIError
		if err := json.Unmarshal(b, &api); err != nil {
			return nil, err
		}
		if api.Code < 0 {
			failures = append(failures, fmt.Errorf("cancel member %d: %w", i, &api))
			continue
		}
		v := new(RespType)
		if err := json.Unmarshal(b, v); err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, errors.Join(failures...)
}
