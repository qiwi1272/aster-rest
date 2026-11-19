package asterRest

type TickerPrice struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
	Time   int64  `json:"time"`
}

type RateLimit struct {
	Type        string `json:"rateLimitType"`
	Interval    string `json:"interval"`
	IntervalNum uint8  `json:"intervalNum"`
	Limit       uint32 `json:"limit"`
}

type Asset struct {
	Asset             string `json:"asset"`
	MarginAvailable   bool   `json:"maginAvailable,omitempty"`
	AutoAssetExchange string `json:"autoAssetExchange,omitempty"`
}

type Filter struct { // TODO: We may care about more values than exist here at somepoint in the future
	Type           string `json:"filterType"` // TODO: enum ??
	TickSize       string `json:"tickSize,omitempty"`
	StepSize       string `json:"stepSize,omitempty"`
	MinPrice       string `json:"minPrice,omitempty"`
	MaxPrice       string `json:"maxPrice,omitempty"`
	MinQty         string `json:"minQty,omitempty"`
	MaxQty         string `json:"maxQty,omitempty"`
	Notional       string `json:"notional,omitempty"`
	MultiplierUp   string `json:"multiplierUp,omitempty"`
	MultiplierDown string `json:"multiplierDown,omitempty"`
	MinNotional    string `json:"minNotional,omitempty"`
	MaxNotional    string `json:"maxNotional,omitempty"`
	Limit          uint32 `json:"limit,omitempty"`
}

type Symbol struct { // TODO: We may care about more values than exist here at somepoint in the future
	Symbol      string        `json:"symbol"`
	BaseAsset   string        `json:"baseAsset"`
	QuoteAsset  string        `json:"quoteAsset"`
	Status      string        `json:"status"`
	Filters     []Filter      `json:"filters"`
	OrderTypes  []OrderType   `json:"orderTypes"`
	TimeInForce []TimeInForce `json:"timeInForce"`
}

type ExchangeInfo struct {
	Timezone        string      `json:"timezone"`
	ServerTime      uint64      `json:"serverTime"`
	RateLimits      []RateLimit `json:"rateLimits"`
	ExchangeFilters []string    `json:"exchangeFilters"`
	Assets          []Asset     `json:"assets"`
	Symbols         []Symbol    `json:"symbols"`
}
