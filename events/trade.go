package events

// TradeEvent describes a position open, partial close, or full close.
type TradeEvent struct {
	TraderID     string
	ExchangeType string
	Symbol       string
	Side         string // LONG or SHORT
	Action       string // open_long, open_short, close_long, close_short
	Quantity     float64
	Price        float64
	RealizedPnL  float64
	OrderID      string
	PartialClose bool
	Leverage     float64
}

var tradeHandler func(TradeEvent)
var tradeListeners []func(TradeEvent)

// OnTrade registers the primary trade callback (e.g. Telegram notifier).
func OnTrade(h func(TradeEvent)) {
	tradeHandler = h
}

// AddTradeListener registers an additional trade callback (not replaced by OnTrade).
func AddTradeListener(h func(TradeEvent)) {
	if h == nil {
		return
	}
	tradeListeners = append(tradeListeners, h)
}

// EmitTrade notifies registered handlers asynchronously.
func EmitTrade(e TradeEvent) {
	if tradeHandler != nil {
		go tradeHandler(e)
	}
	for _, h := range tradeListeners {
		if h != nil {
			go h(e)
		}
	}
}
