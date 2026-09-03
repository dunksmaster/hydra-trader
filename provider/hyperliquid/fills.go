package hyperliquid

import (
	"fmt"
	"strconv"
	"strings"
)

// LeaderFill is a normalized leader trade fill for copy mirroring.
type LeaderFill struct {
	Coin        string
	Symbol      string
	Action      string // open_long | open_short | close_long | close_short
	Price       float64
	Size        float64
	NotionalUSD float64
	Tid         int64
	Hash        string
	Time        int64
}

// ParseFillAction maps Hyperliquid Dir (+ side / closed PnL fallback) to a copy action.
func ParseFillAction(dir, side string, closedPnl float64) string {
	switch strings.ToLower(strings.TrimSpace(dir)) {
	case "open long":
		return "open_long"
	case "open short":
		return "open_short"
	case "close long":
		return "close_long"
	case "close short":
		return "close_short"
	}

	side = strings.ToUpper(strings.TrimSpace(side))
	isBuy := side == "B" || side == "BUY" || side == "BID"
	if closedPnl != 0 {
		if isBuy {
			return "close_short"
		}
		return "close_long"
	}
	if isBuy {
		return "open_long"
	}
	return "open_short"
}

// ParseLeaderFill builds a LeaderFill from raw Hyperliquid fill fields.
func ParseLeaderFill(coin, dir, side, px, sz, closedPnlStr, hash string, tid, timeMs int64) (*LeaderFill, error) {
	coin = strings.TrimSpace(coin)
	if coin == "" {
		return nil, fmt.Errorf("fill coin is required")
	}
	price, err := strconv.ParseFloat(strings.TrimSpace(px), 64)
	if err != nil || price <= 0 {
		return nil, fmt.Errorf("invalid fill price %q", px)
	}
	size, err := strconv.ParseFloat(strings.TrimSpace(sz), 64)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("invalid fill size %q", sz)
	}
	closedPnl, _ := strconv.ParseFloat(strings.TrimSpace(closedPnlStr), 64)
	action := ParseFillAction(dir, side, closedPnl)
	symbol := coinToSymbol(coin)
	return &LeaderFill{
		Coin:        coin,
		Symbol:      symbol,
		Action:      action,
		Price:       price,
		Size:        size,
		NotionalUSD: price * size,
		Tid:         tid,
		Hash:        strings.TrimSpace(hash),
		Time:        timeMs,
	}, nil
}
