package asterRest

type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
	OrderStatusRejected        OrderStatus = "REJECTED"
	OrderStatusExpired         OrderStatus = "EXPIRED"
)

type OrderType string

const (
	OrderTypeLimit              OrderType = "LIMIT"
	OrderTypeMarket             OrderType = "MARKET"
	OrderTypeStop               OrderType = "STOP"        // limit
	OrderTypeTakeProfit         OrderType = "TAKE_PROFIT" // limit
	OrderTypeStopLimit          OrderType = "STOP_MARKET"
	OrderTypeTakeProfitMarket   OrderType = "TAKE_PROFIT_MARKET"
	OrderTypeTrailingStopMarket OrderType = "TRAILING_STOP_MARKET"
)

type OrderResponseType string

const (
	OrderResponseTypeAck    OrderResponseType = "ACK"
	OrderResponseTypeResult OrderResponseType = "RESULT"
	OrderResponseTypeFull   OrderResponseType = "FULL"
)

type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

type TimeInForce string

const (
	TimeInForceGTC    TimeInForce = "GTC"    // Good Till Cancel
	TimeInForceIOC    TimeInForce = "IOC"    // Immediate Or Cancel
	TimeInForceFOK    TimeInForce = "FOK"    // Fill or Kill
	TimeInForceGTX    TimeInForce = "GTX"    // Good Till Crossing
	TimeInForceHidden TimeInForce = "HIDDEN" // Hidden
)

type IntervalType string

const (
	IntervalType1m  IntervalType = "1m"
	IntervalType3m  IntervalType = "3m"
	IntervalType5m  IntervalType = "5m"
	IntervalType15m IntervalType = "15m"
	IntervalType30m IntervalType = "30m"
	IntervalType1h  IntervalType = "1h"
	IntervalType2h  IntervalType = "2h"
	IntervalType4h  IntervalType = "4h"
	IntervalType6h  IntervalType = "6h"
	IntervalType8h  IntervalType = "8h"
	IntervalType12h IntervalType = "12h"
	IntervalType1d  IntervalType = "1d"
	IntervalType3d  IntervalType = "3d"
	IntervalType1w  IntervalType = "1w"
	IntervalType1M  IntervalType = "1M"
)
