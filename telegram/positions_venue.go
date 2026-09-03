package telegram

import (
	"fmt"
	"sort"
	"strings"
)

// VenuePosition is one deduplicated live position on an exchange wallet.
type VenuePosition struct {
	Symbol        string
	Side          string
	Raw           map[string]any
	CloseTraderID string
}

// VenueGroup groups positions that share one exchange account.
type VenueGroup struct {
	Exchange  string
	Label     string
	Bots      []string
	Positions []VenuePosition
	FetchErrs []string
}

func normalizeExchangeKey(exchange string) string {
	return strings.ToLower(strings.TrimSpace(exchange))
}

func venueLabel(exchange string) string {
	switch normalizeExchangeKey(exchange) {
	case "hyperliquid":
		return "Hyperliquid"
	case "bitget":
		return "Bitget"
	case "binance":
		return "Binance"
	case "bybit":
		return "Bybit"
	case "okx":
		return "OKX"
	case "gate":
		return "Gate"
	case "kucoin":
		return "KuCoin"
	case "aster":
		return "Aster"
	case "lighter":
		return "Lighter"
	default:
		ex := strings.TrimSpace(exchange)
		if ex == "" {
			return "Exchange"
		}
		return strings.ToUpper(ex[:1]) + ex[1:]
	}
}

func venueAlias(exchange string) string {
	switch normalizeExchangeKey(exchange) {
	case "hyperliquid":
		return "hl"
	case "bitget":
		return "bg"
	case "binance":
		return "bn"
	case "bybit":
		return "bb"
	case "okx":
		return "okx"
	case "gate":
		return "gt"
	case "kucoin":
		return "kc"
	case "aster":
		return "ast"
	case "lighter":
		return "lt"
	default:
		ex := normalizeExchangeKey(exchange)
		if ex == "" {
			return "ex"
		}
		if len(ex) > 6 {
			return ex[:6]
		}
		return ex
	}
}

func venueMatchesQuery(exchange, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	ex := normalizeExchangeKey(exchange)
	if ex == query {
		return true
	}
	if venueAlias(exchange) == query {
		return true
	}
	if strings.Contains(ex, query) {
		return true
	}
	if strings.Contains(strings.ToLower(venueLabel(exchange)), query) {
		return true
	}
	return false
}

func positionDedupeKey(exchange, symbol, side string) string {
	return normalizeExchangeKey(exchange) + "|" +
		strings.ToUpper(strings.TrimSpace(symbol)) + "|" +
		strings.ToLower(strings.TrimSpace(side))
}

func traderExchangeKey(tp TraderPortfolio) string {
	if ex := normalizeExchangeKey(tp.Info.Exchange); ex != "" {
		return ex
	}
	return strings.ToLower(strings.TrimSpace(inferVenue(tp.Info.TraderName)))
}

func groupPositionsByVenue(portfolios []TraderPortfolio) []VenueGroup {
	type venueAcc struct {
		exchange  string
		bots      map[string]struct{}
		fetchErrs []string
		posByKey  map[string]*VenuePosition
		posOrder  []string
	}

	byExchange := make(map[string]*venueAcc)
	exchangeOrder := make([]string, 0)

	for _, tp := range portfolios {
		exKey := traderExchangeKey(tp)
		if exKey == "" {
			exKey = "unknown"
		}
		acc, ok := byExchange[exKey]
		if !ok {
			acc = &venueAcc{
				exchange: exKey,
				bots:     make(map[string]struct{}),
				posByKey: make(map[string]*VenuePosition),
			}
			byExchange[exKey] = acc
			exchangeOrder = append(exchangeOrder, exKey)
		}
		if tp.Info.TraderName != "" {
			acc.bots[tp.Info.TraderName] = struct{}{}
		}
		if tp.FetchErr != "" {
			acc.fetchErrs = append(acc.fetchErrs, tp.FetchErr)
			continue
		}
		for _, p := range tp.Positions {
			symbol := posString(p, "symbol")
			side := strings.ToLower(posString(p, "side", "position_side"))
			if symbol == "" || side == "" {
				continue
			}
			key := positionDedupeKey(exKey, symbol, side)
			if existing, found := acc.posByKey[key]; found {
				if existing.CloseTraderID == "" && tp.Info.IsRunning {
					existing.CloseTraderID = tp.Info.TraderID
				}
				continue
			}
			closeID := tp.Info.TraderID
			if !tp.Info.IsRunning {
				closeID = ""
			}
			acc.posByKey[key] = &VenuePosition{
				Symbol:        symbol,
				Side:          side,
				Raw:           p,
				CloseTraderID: closeID,
			}
			acc.posOrder = append(acc.posOrder, key)
		}
	}

	out := make([]VenueGroup, 0, len(exchangeOrder))
	for _, exKey := range exchangeOrder {
		acc := byExchange[exKey]
		bots := make([]string, 0, len(acc.bots))
		for name := range acc.bots {
			bots = append(bots, name)
		}
		sort.Strings(bots)

		positions := make([]VenuePosition, 0, len(acc.posOrder))
		for _, key := range acc.posOrder {
			if vp := acc.posByKey[key]; vp != nil {
				if vp.CloseTraderID == "" {
					for _, tp := range portfolios {
						if traderExchangeKey(tp) != exKey || tp.FetchErr != "" {
							continue
						}
						vp.CloseTraderID = tp.Info.TraderID
						break
					}
				}
				positions = append(positions, *vp)
			}
		}

		label := venueLabel(acc.exchange)
		if len(bots) == 1 {
			label += " · " + bots[0]
		} else if len(bots) > 1 {
			label += fmt.Sprintf(" · shared wallet (%d bots)", len(bots))
		}

		out = append(out, VenueGroup{
			Exchange:  acc.exchange,
			Label:     label,
			Bots:      bots,
			Positions: positions,
			FetchErrs: acc.fetchErrs,
		})
	}
	return out
}

func countVenuePositions(groups []VenueGroup) int {
	total := 0
	for _, g := range groups {
		total += len(g.Positions)
	}
	return total
}

func closeCommandForPosition(vp VenuePosition, exchange string) string {
	sym := displaySymbol(vp.Symbol)
	if strings.HasPrefix(strings.ToLower(vp.Symbol), "xyz:") {
		sym = vp.Symbol
	}
	return fmt.Sprintf("/close %s %s %s", sym, vp.Side, venueAlias(exchange))
}
