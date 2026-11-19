package asterRest

import (
	"strings"

	"github.com/shopspring/decimal"
)

type Market struct {
	Ticker, Base, Quote, Upper                string
	Status                                    string
	MinNotional, MultiplierUp, MultiplierDown decimal.Decimal
	Mark                                      decimal.Decimal
	Lot, Step                                 decimal.Decimal
	MinP, MaxP                                decimal.Decimal
	MinQL, MaxQL                              decimal.Decimal
	MinQM, MaxQM                              decimal.Decimal
}

func (s *Symbol) MarketFromSymbol() (*Market, error) {
	out := &Market{
		Ticker: s.Symbol,
		Status: s.Status,
		Base:   s.BaseAsset,
		Quote:  s.QuoteAsset,
		Upper:  strings.ToUpper(s.Symbol),
	}

	var err error
	for _, f := range s.Filters {
		switch f.Type {
		case "MIN_NOTIONAL":
			v := f.Notional
			if v == "" {
				v = f.MinNotional
			}
			out.MinNotional, err = decimal.NewFromString(v)
			if err != nil {
				return nil, err
			}
		case "PERCENT_PRICE":
			out.MultiplierUp, err = decimal.NewFromString(f.MultiplierUp)
			if err != nil {
				return nil, err
			}
			out.MultiplierDown, err = decimal.NewFromString(f.MultiplierDown)
			if err != nil {
				return nil, err
			}
		case "LOT_SIZE":
			out.Lot, err = decimal.NewFromString(f.StepSize)
			if err != nil {
				return nil, err
			}
			out.MinQL, err = decimal.NewFromString(f.MinQty)
			if err != nil {
				return out, err
			}
			out.MaxQL, err = decimal.NewFromString(f.MaxQty)
			if err != nil {
				return out, err
			}
		case "PRICE_FILTER":
			out.Step, err = decimal.NewFromString(f.TickSize)
			if err != nil {
				return out, err
			}
			out.MinP, err = decimal.NewFromString(f.MinPrice)
			if err != nil {
				return out, err
			}
			out.MaxP, err = decimal.NewFromString(f.MaxPrice)
			if err != nil {
				return out, err
			}
		case "MARKET_LOT_SIZE":
			out.MinQM, err = decimal.NewFromString(f.MinQty)
			if err != nil {
				return out, err
			}
			out.MaxQM, err = decimal.NewFromString(f.MaxQty)
			if err != nil {
				return out, err
			}
		}
	}
	return out, nil
}
