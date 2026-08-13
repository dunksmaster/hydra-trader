package telegram

import (
	"encoding/json"
	"fmt"
	"math"
	"nofx/events"
	"nofx/store"
	"strings"
)

// AccountSnapshot holds parsed /api/account fields for formatting.
type AccountSnapshot struct {
	TotalEquity      float64
	AvailableBalance float64
	TotalPnL         float64
	TotalPnLPct      float64
	UnrealizedProfit float64
	MarginUsed       float64
	MarginUsedPct    float64
	PositionCount    int
	DailyPnL         float64
	InitialBalance   float64
	TraderName       string
}

// ParseAccountSnapshot builds AccountSnapshot from API JSON map.
func ParseAccountSnapshot(acct map[string]any, traderName string) AccountSnapshot {
	s := AccountSnapshot{TraderName: traderName}
	s.TotalEquity = jsonNum(acct["total_equity"])
	s.AvailableBalance = jsonNum(acct["available_balance"])
	s.TotalPnL = jsonNum(acct["total_pnl"])
	s.TotalPnLPct = jsonNum(acct["total_pnl_pct"])
	s.UnrealizedProfit = jsonNum(acct["unrealized_profit"])
	s.MarginUsed = jsonNum(acct["margin_used"])
	s.MarginUsedPct = jsonNum(acct["margin_used_pct"])
	s.DailyPnL = jsonNum(acct["daily_pnl"])
	s.InitialBalance = jsonNum(acct["initial_balance"])
	if n, ok := acct["position_count"].(float64); ok {
		s.PositionCount = int(n)
	} else if n, ok := acct["position_count"].(int); ok {
		s.PositionCount = n
	}
	return s
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func pnlIndicator(pnl float64) string {
	if pnl > 0.005 {
		return "🟢"
	}
	if pnl < -0.005 {
		return "🔴"
	}
	return "⚪"
}

func formatMoney(v float64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	if v >= 100 {
		return fmt.Sprintf("%s$%.2f", sign, v)
	}
	if v >= 1 {
		return fmt.Sprintf("%s$%.4f", sign, v)
	}
	return fmt.Sprintf("%s$%.6f", sign, v)
}

func formatMoneySigned(v float64) string {
	if v >= 0 {
		return "+" + formatMoney(v)[1:] // strip leading $ duplication
	}
	return formatMoney(v)
}

func formatPct(v float64) string {
	sign := "+"
	if v < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.2f%%", sign, v)
}

func formatSideBadge(side string) string {
	switch strings.ToUpper(side) {
	case "LONG":
		return "📈 LONG"
	case "SHORT":
		return "📉 SHORT"
	default:
		return strings.ToUpper(side)
	}
}

func displaySymbol(symbol string) string {
	s := strings.ToUpper(symbol)
	return strings.TrimSuffix(s, "USDT")
}

func jsonNum(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

func posFloat(p map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := p[k]; ok {
			if f := jsonNum(v); f != 0 || v == float64(0) {
				return f
			}
		}
	}
	return 0
}

func posString(p map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := p[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// formatPortfolioBlock renders the portfolio tree section.
func formatPortfolioBlock(acct AccountSnapshot, lang string, title string) string {
	pnlInd := pnlIndicator(acct.TotalPnL)
	uInd := pnlIndicator(acct.UnrealizedProfit)

	var sb strings.Builder
	if title != "" {
		sb.WriteString(escapeHTML(title))
		sb.WriteString("\n\n")
	}

	if lang == "zh" {
		sb.WriteString("💰 <b>账户</b>\n")
		sb.WriteString(fmt.Sprintf("┣ 总权益: <b>%s</b>\n", formatMoney(acct.TotalEquity)))
		sb.WriteString(fmt.Sprintf("┣ 总盈亏: %s <b>%s</b> (%s)\n", pnlInd, formatMoney(acct.TotalPnL), formatPct(acct.TotalPnLPct)))
		sb.WriteString(fmt.Sprintf("┣ 未实现: %s <b>%s</b>\n", uInd, formatMoney(acct.UnrealizedProfit)))
		sb.WriteString(fmt.Sprintf("┣ 可用: <b>%s</b>\n", formatMoney(acct.AvailableBalance)))
		sb.WriteString(fmt.Sprintf("┗ 持仓: <b>%d</b> · 保证金: <b>%s</b>", acct.PositionCount, formatMoney(acct.MarginUsed)))
		if acct.TraderName != "" {
			sb.WriteString(fmt.Sprintf("\n\n🤖 <i>%s</i>", escapeHTML(acct.TraderName)))
		}
		return sb.String()
	}

	sb.WriteString("💰 <b>Portfolio</b>\n")
	sb.WriteString(fmt.Sprintf("┣ Total Value: <b>%s</b>\n", formatMoney(acct.TotalEquity)))
	sb.WriteString(fmt.Sprintf("┣ PnL: %s <b>%s</b> (%s)\n", pnlInd, formatMoney(acct.TotalPnL), formatPct(acct.TotalPnLPct)))
	sb.WriteString(fmt.Sprintf("┣ Unrealized: %s <b>%s</b>\n", uInd, formatMoney(acct.UnrealizedProfit)))
	sb.WriteString(fmt.Sprintf("┣ Available: <b>%s</b>\n", formatMoney(acct.AvailableBalance)))
	sb.WriteString(fmt.Sprintf("┗ Positions: <b>%d</b> · Margin: <b>%s</b>", acct.PositionCount, formatMoney(acct.MarginUsed)))
	if acct.TraderName != "" {
		sb.WriteString(fmt.Sprintf("\n\n🤖 <i>%s</i>", escapeHTML(acct.TraderName)))
	}
	return sb.String()
}

// formatPositionsHeader returns the open-positions title line.
func formatPositionsHeader(count int, lang string) string {
	if lang == "zh" {
		return fmt.Sprintf("📈 <b>持仓</b> (%d 个)", count)
	}
	return fmt.Sprintf("📈 <b>Open Positions</b> (%d total)", count)
}

// formatPositionBlock renders one position in tree style.
func formatPositionBlock(p map[string]any, lang string) string {
	symbol := posString(p, "symbol")
	side := posString(p, "side", "position_side")
	qty := posFloat(p, "quantity", "position_amt", "positionAmt")
	entry := posFloat(p, "entry_price", "entryPrice")
	mark := posFloat(p, "mark_price", "markPrice")
	if mark == 0 && entry > 0 {
		mark = entry
	}
	upnl := posFloat(p, "unrealized_pnl", "unRealizedProfit")
	upnlPct := posFloat(p, "unrealized_pnl_pct")
	lev := posFloat(p, "leverage")
	if lev == 0 {
		lev = 1
	}
	margin := posFloat(p, "margin_used")
	if margin == 0 && mark > 0 && qty > 0 {
		margin = (qty * mark) / lev
	}
	liq := posFloat(p, "liquidation_price", "liquidationPrice")
	notional := qty * mark
	ind := pnlIndicator(upnl)

	symDisplay := escapeHTML(displaySymbol(symbol))
	if strings.HasPrefix(strings.ToLower(symbol), "xyz:") {
		symDisplay = escapeHTML(symbol)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b> (%s)\n", symDisplay, formatSideBadge(side)))
	sb.WriteString(fmt.Sprintf("┣ 📊 %.4f @ %s → %s now\n", qty, formatMoney(entry), formatMoney(mark)))
	sb.WriteString(fmt.Sprintf("┣ 💰 %s notional · %.0fx · margin %s\n", formatMoney(notional), lev, formatMoney(margin)))
	sb.WriteString(fmt.Sprintf("┣ %s uPnL <b>%s</b> (%s)", ind, formatMoney(upnl), formatPct(upnlPct)))
	if liq > 0 {
		sb.WriteString(fmt.Sprintf("\n┗ ⚠️ Liq %s", formatMoney(liq)))
	} else {
		sb.WriteString("\n┗")
	}
	return sb.String()
}

// formatRichPositions combines portfolio header, portfolio block, and position blocks.
func formatRichPositions(acct AccountSnapshot, positions []map[string]any, lang string) string {
	if len(positions) == 0 {
		header := formatPortfolioBlock(acct, lang, "")
		if lang == "zh" {
			return header + "\n\n📊 暂无持仓。"
		}
		return header + "\n\n📊 No open positions."
	}

	var sb strings.Builder
	sb.WriteString(formatPositionsHeader(len(positions), lang))
	sb.WriteString("\n\n")
	sb.WriteString(formatPortfolioBlock(acct, lang, ""))
	sb.WriteString("\n\n")
	for i, p := range positions {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(formatPositionBlock(p, lang))
	}
	return sb.String()
}

// formatPortfolioFooterOneLine is a compact summary for trade alert footers.
func formatPortfolioFooterOneLine(acct AccountSnapshot) string {
	uInd := pnlIndicator(acct.UnrealizedProfit)
	return fmt.Sprintf("Equity <b>%s</b> · uPnL %s <b>%s</b> · <b>%d</b> open",
		formatMoney(acct.TotalEquity), uInd, formatMoney(acct.UnrealizedProfit), acct.PositionCount)
}

// formatTradeAlertRich renders an expanded trade notification.
func formatTradeAlertRich(st *store.Store, e events.TradeEvent, lang string, footer *AccountSnapshot) string {
	traderName := lookupTraderName(st, e.TraderID)
	symbol := e.Symbol
	if !strings.HasPrefix(strings.ToLower(symbol), "xyz:") {
		symbol = displaySymbol(symbol)
	}

	var sb strings.Builder

	if e.PartialClose {
		if lang == "zh" {
			sb.WriteString("📉 <b>部分平仓</b>\n\n")
		} else {
			sb.WriteString("📉 <b>Partial close</b>\n\n")
		}
	} else {
		switch e.Action {
		case "open_long", "open_short":
			if lang == "zh" {
				sb.WriteString("📈 <b>开仓</b>\n\n")
			} else {
				sb.WriteString("📈 <b>Position opened</b>\n\n")
			}
		case "close_long", "close_short":
			if lang == "zh" {
				sb.WriteString("✅ <b>平仓</b>\n\n")
			} else {
				sb.WriteString("✅ <b>Position closed</b>\n\n")
			}
		default:
			if strings.HasPrefix(e.Action, "open_") {
				sb.WriteString("📈 <b>Position opened</b>\n\n")
			} else {
				sb.WriteString("✅ <b>Trade update</b>\n\n")
			}
		}
	}

	sb.WriteString(fmt.Sprintf("┣ <b>%s</b> %s\n", escapeHTML(symbol), formatSideBadge(e.Side)))
	sb.WriteString(fmt.Sprintf("┣ 📊 Qty <b>%s</b> @ <b>%s</b>\n", formatQty(e.Quantity), formatPrice(e.Price)))

	if e.Leverage > 0 {
		margin := (e.Quantity * e.Price) / e.Leverage
		sb.WriteString(fmt.Sprintf("┣ 💰 %.0fx · margin ~%s\n", e.Leverage, formatMoney(margin)))
	}

	if strings.HasPrefix(e.Action, "close_") {
		ind := pnlIndicator(e.RealizedPnL)
		sb.WriteString(fmt.Sprintf("┣ %s Realized PnL <b>%s</b>\n", ind, formatMoney(e.RealizedPnL)))
	}

	if traderName != "" {
		sb.WriteString(fmt.Sprintf("┗ 🤖 <i>%s</i>", escapeHTML(traderName)))
	} else {
		sb.WriteString("┗")
	}

	if footer != nil {
		sb.WriteString("\n\n")
		sb.WriteString(formatPortfolioFooterOneLine(*footer))
	}

	return sb.String()
}

func formatQty(q float64) string {
	if q == 0 {
		return "0"
	}
	if q >= 1 {
		return fmt.Sprintf("%.4f", q)
	}
	return fmt.Sprintf("%.6f", q)
}

func formatPrice(p float64) string {
	if p == 0 {
		return "$0"
	}
	abs := p
	if abs < 0 {
		abs = -abs
	}
	if abs >= 100 {
		return fmt.Sprintf("$%.2f", p)
	}
	if abs >= 1 {
		return fmt.Sprintf("$%.4f", p)
	}
	decimals := int(math.Ceil(-math.Log10(abs))) + 2
	if decimals < 2 {
		decimals = 2
	}
	if decimals > 8 {
		decimals = 8
	}
	return fmt.Sprintf("$%.*f", decimals, p)
}
