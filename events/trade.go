package events

// TradeEvent describes a position open, partial close, or full close.
type TradeEvent struct {
	TraderID     string
	ExchangeType string
	Symbol       string
	Side         string  // LONG or SHORT
	Action       string  // open_long, open_short, close_long, close_short
	Quantity     float64
	Price        float64
	RealizedPnL  float64
	OrderID      string
	PartialClose bool
}

var tradeHandler func(TradeEvent)

// OnTrade registers a callback for trade events. Only one handler is kept.
func OnTrade(h func(TradeEvent)) {
	tradeHandler = h
}

// EmitTrade notifies the registered handler asynchronously.
func EmitTrade(e TradeEvent) {
	if tradeHandler == nil {
		return
	}
	go tradeHandler(e)
}
