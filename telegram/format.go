package telegram

import (
	"encoding/json"
	"fmt"
	"math"
	"nofx/events"
	"nofx/store"
	"strings"
	"time"
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
	StrategyName     string
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

// pnlDot is intentionally strict: every positive value is green, every
// negative value is red, and exact zero is neutral.
func pnlDot(pnl float64) string {
	if pnl > 0 {
		return "🟢"
	}
	if pnl < 0 {
		return "🔴"
	}
	return "⚪"
}

func formatClassicAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "open_long":
		return "OPEN LONG"
	case "open_short":
		return "OPEN SHORT"
	case "close_long":
		return "CLOSE LONG"
	case "close_short":
		return "CLOSE SHORT"
	default:
		return strings.ToUpper(strings.TrimSpace(action))
	}
}

func formatMoney(v float64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	if v >= 0.005 {
		return fmt.Sprintf("%s$%.2f", sign, v)
	}
	if v > 0 {
		return fmt.Sprintf("%s$%.4f", sign, v)
	}
	return sign + "$0.00"
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

func formatPnLMoney(v float64) string {
	if v > 0.0049 {
		return fmt.Sprintf("+$%.2f", v)
	}
	if v < -0.0049 {
		return fmt.Sprintf("-$%.2f", -v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func formatCompactQty(q float64) string {
	if q >= 1 {
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", q), "0"), ".")
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.8f", q), "0"), ".")
}

func formatCompactPrice(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("$%.2f", v)
	}
	if v >= 1 {
		return fmt.Sprintf("$%.2f", v)
	}
	return formatMoney(v)
}

func inferVenue(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "bigg"), strings.Contains(n, "bitget"):
		return "Bitget"
	case strings.Contains(n, "autopilot"), strings.Contains(n, "hyper"),
		strings.Contains(n, "leviathan"), strings.Contains(n, "grinder"),
		strings.Contains(n, "money printer"), strings.Contains(n, "copy l4"),
		strings.Contains(n, "alpha 6859"), strings.Contains(n, "alpha"),
		strings.Contains(n, "hyperdash"), strings.Contains(n, "machibig"):
		return "Hyperliquid"
	default:
		return ""
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

// formatPortfolioBlock renders the portfolio tree section (Polymarket-style).
func formatPortfolioBlock(acct AccountSnapshot, stats TradingStats, lang string, title string) string {
	tradingTotal := stats.TotalPnL + acct.UnrealizedProfit
	tradingInd := pnlIndicator(tradingTotal)
	closedInd := pnlIndicator(stats.TotalPnL)
	openInd := pnlIndicator(acct.UnrealizedProfit)
	equityInd := pnlIndicator(acct.TotalPnL)

	var sb strings.Builder
	if title != "" {
		sb.WriteString(escapeHTML(title))
		sb.WriteString("\n\n")
	}

	if lang == "zh" {
		sb.WriteString("💰 <b>账户</b>\n")
		sb.WriteString(fmt.Sprintf("┣ 总权益: <b>%s</b>\n", formatMoney(acct.TotalEquity)))
		if stats.TotalTrades > 0 {
			sb.WriteString(fmt.Sprintf("┣ 已平仓: %s <b>%s</b> (%d 笔 · 手续费 %s)\n",
				closedInd, formatMoney(stats.TotalPnL), stats.TotalTrades, formatMoney(stats.TotalFee)))
		}
		sb.WriteString(fmt.Sprintf("┣ 持仓浮盈: %s <b>%s</b>\n", openInd, formatMoney(acct.UnrealizedProfit)))
		sb.WriteString(fmt.Sprintf("┣ 交易合计: %s <b>%s</b>\n", tradingInd, formatMoney(tradingTotal)))
		if acct.InitialBalance > 0 {
			sb.WriteString(fmt.Sprintf("┣ 相对起始: %s <b>%s</b> (%s)\n",
				equityInd, formatMoney(acct.TotalPnL), formatPct(acct.TotalPnLPct)))
			sb.WriteString(fmt.Sprintf("┣ 起始基准: %s\n", formatMoney(acct.InitialBalance)))
		}
		sb.WriteString(fmt.Sprintf("┣ 可用: <b>%s</b>\n", formatMoney(acct.AvailableBalance)))
		if stats.TotalTrades > 0 {
			sb.WriteString(fmt.Sprintf("┗ 胜率: <b>%.1f%%</b> · %d 胜 / %d 负",
				stats.WinRate, stats.WinTrades, stats.LossTrades))
		} else {
			sb.WriteString(fmt.Sprintf("┗ 持仓: <b>%d</b> · 保证金: <b>%s</b>", acct.PositionCount, formatMoney(acct.MarginUsed)))
		}
	} else {
		sb.WriteString("💰 <b>Portfolio</b>\n")
		sb.WriteString(fmt.Sprintf("┣ Total Value: <b>%s</b>\n", formatMoney(acct.TotalEquity)))
		if stats.TotalTrades > 0 {
			sb.WriteString(fmt.Sprintf("┣ Closed trades: %s <b>%s</b> (%d trades · fees %s)\n",
				closedInd, formatMoney(stats.TotalPnL), stats.TotalTrades, formatMoney(stats.TotalFee)))
		}
		sb.WriteString(fmt.Sprintf("┣ Open positions: %s <b>%s</b> unrealized\n",
			openInd, formatMoney(acct.UnrealizedProfit)))
		sb.WriteString(fmt.Sprintf("┣ Trading total: %s <b>%s</b>\n", tradingInd, formatMoney(tradingTotal)))
		if acct.InitialBalance > 0 {
			sb.WriteString(fmt.Sprintf("┣ Since start: %s <b>%s</b> (%s)\n",
				equityInd, formatMoney(acct.TotalPnL), formatPct(acct.TotalPnLPct)))
			sb.WriteString(fmt.Sprintf("┣ Start balance: %s\n", formatMoney(acct.InitialBalance)))
		}
		sb.WriteString(fmt.Sprintf("┣ Available: <b>%s</b>\n", formatMoney(acct.AvailableBalance)))
		if stats.TotalTrades > 0 {
			sb.WriteString(fmt.Sprintf("┗ Win rate: <b>%.1f%%</b> · %dW / %dL",
				stats.WinRate, stats.WinTrades, stats.LossTrades))
		} else {
			sb.WriteString(fmt.Sprintf("┗ Positions: <b>%d</b> · Margin: <b>%s</b>", acct.PositionCount, formatMoney(acct.MarginUsed)))
		}
	}

	if acct.TraderName != "" {
		sb.WriteString(fmt.Sprintf("\n\n🤖 <i>%s</i>", escapeHTML(acct.TraderName)))
		if v := inferVenue(acct.TraderName); v != "" {
			sb.WriteString(fmt.Sprintf(" · %s", escapeHTML(v)))
		}
	}
	if acct.StrategyName != "" {
		if lang == "zh" {
			sb.WriteString(fmt.Sprintf("\n📋 策略: <i>%s</i>", escapeHTML(acct.StrategyName)))
		} else {
			sb.WriteString(fmt.Sprintf("\n📋 Strategy: <i>%s</i>", escapeHTML(acct.StrategyName)))
		}
	}
	return sb.String()
}

func traderStatusLine(info TraderInfo, lang string) string {
	status := "stopped"
	if info.IsRunning {
		status = "running"
	}
	if lang == "zh" {
		if info.IsRunning {
			status = "运行中"
		} else {
			status = "已停止"
		}
	}
	venue := inferVenue(info.TraderName)
	if venue != "" {
		line := fmt.Sprintf("<b>%s</b> · %s · %s", escapeHTML(info.TraderName), escapeHTML(venue), status)
		return line
	}
	line := fmt.Sprintf("<b>%s</b> · %s", escapeHTML(info.TraderName), status)
	if info.StrategyName != "" {
		line += fmt.Sprintf("\nStrategy: <i>%s</i>", escapeHTML(info.StrategyName))
	}
	return line
}

func formatTraderPortfolioSection(tp TraderPortfolio, lang string, includePositions bool) string {
	if tp.FetchErr != "" {
		var sb strings.Builder
		sb.WriteString(traderStatusLine(tp.Info, lang))
		sb.WriteString("\n⚠️ ")
		sb.WriteString(escapeHTML(tp.FetchErr))
		return sb.String()
	}

	var sb strings.Builder
	sb.WriteString(formatPortfolioBlock(tp.Snapshot, tp.Stats, lang, ""))
	if !includePositions || len(tp.Positions) == 0 {
		if len(tp.Positions) == 0 {
			if lang == "zh" {
				sb.WriteString("\n\n📊 暂无持仓。")
			} else {
				sb.WriteString("\n\n📊 No open positions.")
			}
		}
		return sb.String()
	}
	sb.WriteString("\n\n")
	for i, p := range tp.Positions {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(formatPositionBlock(p, lang, tp.Info.TraderName))
	}
	return sb.String()
}

func formatTraderPositionsOnly(tp TraderPortfolio, lang string) string {
	if tp.FetchErr != "" {
		return fmt.Sprintf("<b>%s</b>\n⚠️ %s", escapeHTML(tp.Info.TraderName), escapeHTML(tp.FetchErr))
	}
	if len(tp.Positions) == 0 {
		if lang == "zh" {
			return fmt.Sprintf("<b>%s</b>\n📊 暂无持仓。", escapeHTML(tp.Info.TraderName))
		}
		return fmt.Sprintf("<b>%s</b>\n📊 No open positions.", escapeHTML(tp.Info.TraderName))
	}
	var sb strings.Builder
	sb.WriteString(formatBotHeader(tp.Info, lang))
	for i, p := range tp.Positions {
		if i > 0 {
			sb.WriteString("\n\n")
		} else {
			sb.WriteString("\n")
		}
		sb.WriteString(formatPositionBlockCompact(p, lang))
	}
	return sb.String()
}

// formatTraderPositionsList renders one compact row per position beneath its bot.
func formatTraderPositionsList(tp TraderPortfolio, lang string) string {
	if tp.FetchErr != "" {
		return fmt.Sprintf("%s\n└ ⚠️ %s", formatBotHeader(tp.Info, lang), escapeHTML(tp.FetchErr))
	}
	if len(tp.Positions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(formatBotHeader(tp.Info, lang))
	for _, p := range tp.Positions {
		sb.WriteString("\n")
		sb.WriteString(formatPositionListLine(p, lang))
	}
	return sb.String()
}

func formatBotHeader(info TraderInfo, lang string) string {
	venue := inferVenue(info.TraderName)
	if venue != "" {
		return fmt.Sprintf("🤖 <b>%s</b> · %s", escapeHTML(info.TraderName), escapeHTML(venue))
	}
	return fmt.Sprintf("🤖 <b>%s</b>", escapeHTML(info.TraderName))
}

// formatMultiTraderSnapshot stacks every trader for daily snapshot / notify test.
func formatMultiTraderSnapshot(portfolios []TraderPortfolio, lang string, includePositions bool) string {
	if len(portfolios) == 0 {
		if lang == "zh" {
			return "暂无交易员。"
		}
		return "No traders configured."
	}
	var sb strings.Builder
	for i, tp := range portfolios {
		if i > 0 {
			sb.WriteString("\n\n────────────\n\n")
		}
		sb.WriteString(formatTraderPortfolioSection(tp, lang, includePositions))
	}
	return sb.String()
}

// formatBalanceForPortfolios renders portfolio summary for selected trader(s).
func formatBalanceForPortfolios(portfolios []TraderPortfolio, lang string) string {
	title := "💰 Portfolio"
	if lang == "zh" {
		title = "💰 账户"
	}
	if len(portfolios) == 1 && portfolios[0].FetchErr == "" {
		return formatPortfolioBlock(portfolios[0].Snapshot, portfolios[0].Stats, lang, title)
	}
	var sb strings.Builder
	sb.WriteString(escapeHTML(title))
	sb.WriteString("\n\n")
	sb.WriteString(formatMultiTraderSnapshot(portfolios, lang, false))
	return sb.String()
}

// formatPositionsForPortfolios renders positions grouped by exchange wallet.
// Use /pnl for closed-trade stats and /balanca for full account summary.
func formatPositionsForPortfolios(portfolios []TraderPortfolio, lang string) string {
	groups := groupPositionsByVenue(portfolios)
	total := countVenuePositions(groups)

	var sb strings.Builder
	sb.WriteString(formatPositionsHeader(total, lang))
	if len(portfolios) == 0 {
		if lang == "zh" {
			return sb.String() + "\n\n暂无交易员。"
		}
		return sb.String() + "\n\nNo traders configured."
	}

	sections := make([]string, 0, len(groups))
	for _, g := range groups {
		if section := formatVenuePositionsSection(g, lang); section != "" {
			sections = append(sections, section)
		}
	}
	if len(sections) == 0 {
		if lang == "zh" {
			sb.WriteString("\n\n暂无持仓。")
		} else {
			sb.WriteString("\n\nNo open positions.")
		}
	} else {
		sb.WriteString("\n\n")
		sb.WriteString(strings.Join(sections, "\n\n────────────\n\n"))
	}
	if lang == "zh" {
		sb.WriteString("\n\n<i>完整盈亏 → /pnl · 历史 → /history · 余额 → /balanca</i>")
	} else {
		sb.WriteString("\n\n<i>Full PnL → /pnl · History → /history · Balance → /balanca · Close → /close SYMBOL</i>")
	}
	return sb.String()
}

func formatVenuePositionsSection(g VenueGroup, lang string) string {
	if len(g.Positions) == 0 && len(g.FetchErrs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🤖 <b>%s</b>", escapeHTML(g.Label)))
	if len(g.Positions) == 0 {
		if len(g.FetchErrs) > 0 {
			sb.WriteString("\n└ ⚠️ ")
			sb.WriteString(escapeHTML(g.FetchErrs[0]))
		}
		return sb.String()
	}
	for i, vp := range g.Positions {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		sb.WriteString(formatPositionListLine(vp.Raw, lang))
		if vp.CloseTraderID != "" {
			cmd := closeCommand(mintCloseTokenID(vp.CloseTraderID, vp.Symbol, vp.Side))
			if lang == "zh" {
				sb.WriteString(fmt.Sprintf("\n└ 卖出: <code>%s</code>", escapeHTML(cmd)))
			} else {
				sb.WriteString(fmt.Sprintf("\n└ Sell: <code>%s</code>", escapeHTML(cmd)))
			}
		}
	}
	return sb.String()
}

// formatPositionsSummary lists each bot with the real side of every coin.
// It never groups mixed books as "all shorts" or "all longs".
func formatPositionsSummary(portfolios []TraderPortfolio, lang string) string {
	var parts []string
	for _, tp := range portfolios {
		if tp.FetchErr != "" || len(tp.Positions) == 0 {
			continue
		}
		legs := make([]string, 0, len(tp.Positions))
		for _, p := range tp.Positions {
			sym := displaySymbol(posString(p, "symbol"))
			if strings.HasPrefix(strings.ToLower(posString(p, "symbol")), "xyz:") {
				sym = posString(p, "symbol")
			}
			side := strings.ToLower(posString(p, "side", "position_side"))
			if side == "" {
				side = "?"
			}
			legs = append(legs, fmt.Sprintf("%s %s", escapeHTML(sym), escapeHTML(side)))
		}
		parts = append(parts, fmt.Sprintf("<b>%s</b>: %s", escapeHTML(tp.Info.TraderName), strings.Join(legs, " · ")))
	}
	if len(parts) == 0 {
		return ""
	}
	label := "Summary"
	if lang == "zh" {
		label = "摘要"
	}
	return fmt.Sprintf("%s\n%s", label, strings.Join(parts, "\n"))
}

// formatPositionsHeader returns the open-positions title line.
func formatPositionsHeader(count int, lang string) string {
	if lang == "zh" {
		return fmt.Sprintf("📈 <b>持仓</b> (%d 个)", count)
	}
	return fmt.Sprintf("📈 <b>Open Positions</b> (%d total)", count)
}

// formatPositionBlock renders one live trade in Polymarket-style tree layout.
func formatPositionBlock(p map[string]any, lang string, traderName string) string {
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
	if margin == 0 && mark > 0 && qty != 0 {
		q := qty
		if q < 0 {
			q = -q
		}
		margin = (q * mark) / lev
	}
	notional := margin * lev
	currentValue := notional + upnl
	liq := posFloat(p, "liquidation_price", "liquidationPrice")
	ind := pnlIndicator(upnl)

	symDisplay := escapeHTML(displaySymbol(symbol))
	if strings.HasPrefix(strings.ToLower(symbol), "xyz:") {
		symDisplay = escapeHTML(symbol)
	}
	sideLabel := strings.ToUpper(strings.TrimSpace(side))
	if sideLabel == "" {
		sideLabel = "?"
	}
	venue := inferVenue(traderName)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b> (%s)\n", symDisplay, escapeHTML(sideLabel)))
	sb.WriteString(fmt.Sprintf("├ 📊 %s @ %s avg → %s now\n",
		formatCompactQty(qtyAbs(qty)), formatCompactPrice(entry), formatCompactPrice(mark)))
	if lang == "zh" {
		sb.WriteString(fmt.Sprintf("├ 💰 %s 保证金 → %s 现值\n", formatMoney(margin), formatMoney(currentValue)))
		sb.WriteString(fmt.Sprintf("├ %s 浮盈: <b>%s</b> (%s)\n", ind, formatPnLMoney(upnl), formatPct(upnlPct)))
	} else {
		sb.WriteString(fmt.Sprintf("├ 💰 %s margin → %s value\n", formatMoney(margin), formatMoney(currentValue)))
		sb.WriteString(fmt.Sprintf("├ %s Profit: <b>%s</b> (%s)\n", ind, formatPnLMoney(upnl), formatPct(upnlPct)))
	}
	if traderName != "" {
		line := fmt.Sprintf("├ 🤖 %s", escapeHTML(traderName))
		if venue != "" {
			line += fmt.Sprintf(" (%s)", escapeHTML(venue))
		}
		line += fmt.Sprintf(" — %s · %.0fx\n", formatCompactQty(qtyAbs(qty)), lev)
		sb.WriteString(line)
	}
	if lang == "zh" {
		sb.WriteString("└ 卖出 · 盈亏 · SL · TP")
	} else {
		sb.WriteString("└ Sell · PnL · SL · TP")
	}
	if liq > 0 {
		sb.WriteString(fmt.Sprintf(" · Liq %s", formatCompactPrice(liq)))
	}
	return sb.String()
}

// formatPositionBlockCompact is the short /positions view: price + PnL only.
func formatPositionBlockCompact(p map[string]any, lang string) string {
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
	liq := posFloat(p, "liquidation_price", "liquidationPrice")
	ind := pnlIndicator(upnl)

	symDisplay := escapeHTML(displaySymbol(symbol))
	if strings.HasPrefix(strings.ToLower(symbol), "xyz:") {
		symDisplay = escapeHTML(symbol)
	}
	sideLabel := strings.ToUpper(strings.TrimSpace(side))
	if sideLabel == "" {
		sideLabel = "?"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b> (%s)\n", symDisplay, escapeHTML(sideLabel)))
	sb.WriteString(fmt.Sprintf("├ 📊 %s @ %s → %s\n",
		formatCompactQty(qtyAbs(qty)), formatCompactPrice(entry), formatCompactPrice(mark)))
	if lang == "zh" {
		sb.WriteString(fmt.Sprintf("├ %s 浮盈: <b>%s</b> (%s)\n", ind, formatPnLMoney(upnl), formatPct(upnlPct)))
	} else {
		sb.WriteString(fmt.Sprintf("├ %s PnL: <b>%s</b> (%s)\n", ind, formatPnLMoney(upnl), formatPct(upnlPct)))
	}
	if lang == "zh" {
		sb.WriteString(fmt.Sprintf("└ 卖出 · 盈亏 · SL · TP · %.0fx", lev))
	} else {
		sb.WriteString(fmt.Sprintf("└ Sell · PnL · SL · TP · %.0fx", lev))
	}
	if liq > 0 {
		sb.WriteString(fmt.Sprintf(" · Liq %s", formatCompactPrice(liq)))
	}
	return sb.String()
}

// formatPositionListLine is the compact one-row /positions representation (no clock dates).
func formatPositionListLine(p map[string]any, lang string) string {
	symbol := posString(p, "symbol")
	symDisplay := displaySymbol(symbol)
	if strings.HasPrefix(strings.ToLower(symbol), "xyz:") {
		symDisplay = symbol
	}
	side := strings.ToUpper(strings.TrimSpace(posString(p, "side", "position_side")))
	if side == "" {
		side = "?"
	}
	upnl := posFloat(p, "unrealized_pnl", "unRealizedProfit")
	upnlPct := posFloat(p, "unrealized_pnl_pct")
	lev := posFloat(p, "leverage")
	line := fmt.Sprintf("└ %s <b>%s %s</b> • PnL <b>%s</b> (%s)",
		pnlDot(upnl), escapeHTML(symDisplay), escapeHTML(side), formatPnLMoney(upnl), formatPct(upnlPct))
	if lev > 0 {
		line += fmt.Sprintf(" • %.0fx", lev)
	}
	return line
}

// formatOrdersReport is the instant /orders view: classic fills, no clock dates, no AI.
func formatOrdersReport(portfolios []TraderPortfolio, orders map[string][]map[string]any, lang string) string {
	title := "📋 <b>Orders</b>"
	if lang == "zh" {
		title = "📋 <b>订单</b>"
	}
	var sb strings.Builder
	sb.WriteString(title)

	sections := make([]string, 0)
	if openSection := formatOrdersOpenSection(portfolios, lang); openSection != "" {
		sections = append(sections, openSection)
	}

	fillCount := 0
	for _, tp := range portfolios {
		if section := formatTraderFillsBlock(tp, orders[tp.Info.TraderID], lang); section != "" {
			sections = append(sections, section)
			fillCount++
			if fillCount >= 6 {
				break
			}
		}
	}

	if len(sections) == 0 {
		if lang == "zh" {
			sb.WriteString("\n\n暂无订单。")
		} else {
			sb.WriteString("\n\nNo orders yet.")
		}
		return sb.String()
	}
	sb.WriteString("\n\n")
	sb.WriteString(strings.Join(sections, "\n\n────────────\n\n"))
	if fillCount >= 6 && len(portfolios) > fillCount {
		if lang == "zh" {
			sb.WriteString(fmt.Sprintf("\n\n<i>…另有 %d 个机器人，发送 /traders 切换查看</i>", len(portfolios)-fillCount))
		} else {
			sb.WriteString(fmt.Sprintf("\n\n<i>…%d more bots — /traders to filter</i>", len(portfolios)-fillCount))
		}
	}
	return sb.String()
}

func formatOrdersOpenSection(portfolios []TraderPortfolio, lang string) string {
	groups := groupPositionsByVenue(portfolios)
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		if len(g.Positions) == 0 {
			continue
		}
		var part strings.Builder
		part.WriteString(fmt.Sprintf("🤖 <b>%s</b>", escapeHTML(g.Label)))
		part.WriteString("\n")
		if lang == "zh" {
			part.WriteString("📈 持仓")
		} else {
			part.WriteString("📈 Open")
		}
		for _, vp := range g.Positions {
			part.WriteString("\n")
			part.WriteString(formatPositionListLine(vp.Raw, lang))
		}
		parts = append(parts, part.String())
	}
	return strings.Join(parts, "\n\n")
}

func formatTraderFillsBlock(tp TraderPortfolio, rows []map[string]any, lang string) string {
	if tp.FetchErr != "" {
		var sb strings.Builder
		sb.WriteString(formatBotHeader(tp.Info, lang))
		sb.WriteString(fmt.Sprintf("\nCopy trade bot: <b>%s</b>", escapeHTML(tp.Info.TraderName)))
		sb.WriteString("\n└ ⚠️ ")
		sb.WriteString(escapeHTML(tp.FetchErr))
		return sb.String()
	}
	if len(rows) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(formatBotHeader(tp.Info, lang))
	sb.WriteString(fmt.Sprintf("\nCopy trade bot: <b>%s</b>", escapeHTML(tp.Info.TraderName)))
	sb.WriteString("\n")
	if lang == "zh" {
		sb.WriteString("📜 成交")
	} else {
		sb.WriteString("📜 Fills")
	}
	shown := 0
	for _, row := range rows {
		line := formatOrderFillLine(row)
		if line == "" {
			continue
		}
		sb.WriteString("\n")
		sb.WriteString(line)
		shown++
		if shown >= 8 {
			break
		}
	}
	if shown == 0 {
		return ""
	}
	return sb.String()
}

func formatTraderOrdersBlock(tp TraderPortfolio, rows []map[string]any, lang string) string {
	var sb strings.Builder
	sb.WriteString(formatBotHeader(tp.Info, lang))
	sb.WriteString(fmt.Sprintf("\nCopy trade bot: <b>%s</b>", escapeHTML(tp.Info.TraderName)))
	if tp.FetchErr != "" {
		sb.WriteString("\n└ ⚠️ ")
		sb.WriteString(escapeHTML(tp.FetchErr))
		return sb.String()
	}
	wrote := false
	if len(tp.Positions) > 0 {
		sb.WriteString("\n")
		if lang == "zh" {
			sb.WriteString("📈 持仓")
		} else {
			sb.WriteString("📈 Open")
		}
		for _, p := range tp.Positions {
			sb.WriteString("\n")
			sb.WriteString(formatPositionListLine(p, lang))
		}
		wrote = true
	}
	if len(rows) > 0 {
		if wrote {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		if lang == "zh" {
			sb.WriteString("📜 成交")
		} else {
			sb.WriteString("📜 Fills")
		}
		shown := 0
		for _, row := range rows {
			line := formatOrderFillLine(row)
			if line == "" {
				continue
			}
			sb.WriteString("\n")
			sb.WriteString(line)
			shown++
			if shown >= 8 {
				break
			}
		}
		wrote = wrote || shown > 0
	}
	if !wrote {
		if lang == "zh" {
			sb.WriteString("\n└ 暂无订单。")
		} else {
			sb.WriteString("\n└ No orders.")
		}
	}
	return sb.String()
}

func formatOrderFillLine(row map[string]any) string {
	symbol := posString(row, "symbol")
	if symbol == "" {
		return ""
	}
	action := posString(row, "order_action", "action")
	if action == "" {
		side := strings.ToUpper(posString(row, "side", "position_side"))
		typ := strings.ToUpper(posString(row, "type"))
		if strings.Contains(typ, "CLOSE") || posString(row, "reduce_only") == "true" {
			if strings.Contains(side, "SHORT") {
				action = "close_short"
			} else {
				action = "close_long"
			}
		} else if strings.Contains(side, "SHORT") {
			action = "open_short"
		} else {
			action = "open_long"
		}
	}
	status := strings.ToUpper(strings.TrimSpace(posString(row, "status")))
	if status == "" {
		status = "FILLED"
	}
	qty := posFloat(row, "filled_quantity", "quantity")
	price := posFloat(row, "avg_fill_price", "price")
	lev := posFloat(row, "leverage")
	realizedPnL := posFloat(row, "realized_pnl", "realizedPnl")
	line := fmt.Sprintf("└ %s %s  %s\n  %s — %s @ %s",
		pnlDot(realizedPnL), formatClassicAction(action), escapeHTML(strings.ToUpper(symbol)),
		escapeHTML(status), formatQty(qty), formatPrice(price))
	if lev > 1 {
		line += fmt.Sprintf(" · %.0fx", lev)
	}
	return line
}

func qtyAbs(q float64) float64 {
	if q < 0 {
		return -q
	}
	return q
}

// formatRichPositions combines portfolio header, portfolio block, and position blocks.
func formatRichPositions(acct AccountSnapshot, positions []map[string]any, lang string) string {
	if len(positions) == 0 {
		header := formatPortfolioBlock(acct, TradingStats{}, lang, "")
		if lang == "zh" {
			return header + "\n\n📊 暂无持仓。"
		}
		return header + "\n\n📊 No open positions."
	}

	var sb strings.Builder
	sb.WriteString(formatPositionsHeader(len(positions), lang))
	sb.WriteString("\n\n")
	sb.WriteString(formatPortfolioBlock(acct, TradingStats{}, lang, ""))
	sb.WriteString("\n\n")
	for i, p := range positions {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(formatPositionBlock(p, lang, acct.TraderName))
	}
	return sb.String()
}

func closedTradeNet(t ClosedTrade) float64 {
	return t.RealizedPnL - t.Fee
}

func formatExitTime(ms int64, lang string) string {
	if ms <= 0 {
		return "—"
	}
	t := time.UnixMilli(ms).UTC()
	if lang == "zh" {
		return t.Format("01-02 15:04 UTC")
	}
	return t.Format("Jan 2, 15:04 UTC")
}

// formatTradeHistoryReport lists recent closed trades; losses are marked clearly.
func formatTradeHistoryReport(histories []TraderHistory, lang string, lossesOnly bool) string {
	title := "📜 Closed trade history"
	if lossesOnly {
		title = "📜 Losing trades"
		if lang == "zh" {
			title = "📜 亏损交易"
		}
	} else if lang == "zh" {
		title = "📜 已平仓记录"
	}
	if len(histories) == 0 {
		if lang == "zh" {
			return title + "\n\n暂无交易员。"
		}
		return title + "\n\nNo traders configured."
	}

	var sb strings.Builder
	sb.WriteString("<b>" + title + "</b>\n")
	for i, th := range histories {
		if i > 0 {
			sb.WriteString("\n\n────────────\n\n")
		}
		sb.WriteString(formatTraderHistoryBlock(th, lang, lossesOnly))
	}
	if lang == "zh" {
		sb.WriteString("\n\n<i>红色为净亏损。若与交易所不一致，以交易所为准。</i>")
	} else {
		sb.WriteString("\n\n<i>Red = net loss after fees. If a row looks wrong, trust the exchange app.</i>")
	}
	return sb.String()
}

func formatTraderHistoryBlock(th TraderHistory, lang string, lossesOnly bool) string {
	if th.FetchErr != "" {
		return traderStatusLine(th.Info, lang) + "\n⚠️ " + escapeHTML(th.FetchErr)
	}

	trades := th.Trades
	if lossesOnly {
		filtered := make([]ClosedTrade, 0, len(trades))
		for _, t := range trades {
			if closedTradeNet(t) < -0.005 {
				filtered = append(filtered, t)
			}
		}
		trades = filtered
	}

	var sb strings.Builder
	sb.WriteString(formatBotHeader(th.Info, lang))
	sb.WriteString("\n")

	if th.Stats.TotalTrades > 0 {
		ind := pnlIndicator(th.Stats.TotalPnL)
		if lang == "zh" {
			sb.WriteString(fmt.Sprintf("已平仓 %d 笔 · %d 胜 %d 负 · 净 %s <b>%s</b>\n",
				th.Stats.TotalTrades, th.Stats.WinTrades, th.Stats.LossTrades, ind, formatMoney(th.Stats.TotalPnL)))
		} else {
			sb.WriteString(fmt.Sprintf("Closed %d · %dW %dL · net %s <b>%s</b>\n",
				th.Stats.TotalTrades, th.Stats.WinTrades, th.Stats.LossTrades, ind, formatMoney(th.Stats.TotalPnL)))
		}
	}

	if len(trades) == 0 {
		if lossesOnly {
			if lang == "zh" {
				sb.WriteString("\n✅ 最近记录中没有亏损交易。")
			} else {
				sb.WriteString("\n✅ No losing trades in recent history.")
			}
		} else if lang == "zh" {
			sb.WriteString("\n暂无已平仓记录。")
		} else {
			sb.WriteString("\nNo closed trades yet.")
		}
		return sb.String()
	}

	sb.WriteString("\n")
	for i, t := range trades {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(formatClosedTradeLine(t, i+1, lang))
	}
	return sb.String()
}

func formatClosedTradeLine(t ClosedTrade, num int, lang string) string {
	net := closedTradeNet(t)
	ind := pnlIndicator(net)
	symbol := displaySymbol(t.Symbol)
	if strings.HasPrefix(strings.ToLower(t.Symbol), "xyz:") {
		symbol = strings.ToUpper(t.Symbol)
	}
	side := strings.ToUpper(strings.TrimSpace(t.Side))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s <b>#%d %s %s</b>  net <b>%s</b>\n",
		ind, num, escapeHTML(symbol), formatSideBadge(side), formatPnLMoney(net)))
	sb.WriteString(fmt.Sprintf("├ %s → %s\n", formatCompactPrice(t.EntryPrice), formatCompactPrice(t.ExitPrice)))
	if lang == "zh" {
		sb.WriteString(fmt.Sprintf("├ 毛盈亏 %s · 手续费 %s\n", formatPnLMoney(t.RealizedPnL), formatMoney(t.Fee)))
	} else {
		sb.WriteString(fmt.Sprintf("├ gross %s · fee %s\n", formatPnLMoney(t.RealizedPnL), formatMoney(t.Fee)))
	}
	sb.WriteString(fmt.Sprintf("┗ %s", formatExitTime(t.ExitTime, lang)))
	if t.Leverage > 1 {
		sb.WriteString(fmt.Sprintf(" · %dx", t.Leverage))
	}
	return sb.String()
}

// formatPnLReport shows closed + open PnL for every bot (no AI, instant).
func formatPnLReport(portfolios []TraderPortfolio, lang string) string {
	title := "📊 Trading P&amp;L"
	if lang == "zh" {
		title = "📊 交易盈亏"
	}
	if len(portfolios) == 0 {
		if lang == "zh" {
			return title + "\n\n暂无交易员。"
		}
		return title + "\n\nNo traders configured."
	}
	var sb strings.Builder
	sb.WriteString("<b>" + title + "</b>\n\n")
	for i, tp := range portfolios {
		if i > 0 {
			sb.WriteString("\n\n────────────\n\n")
		}
		if tp.FetchErr != "" {
			sb.WriteString(traderStatusLine(tp.Info, lang))
			sb.WriteString("\n⚠️ ")
			sb.WriteString(escapeHTML(tp.FetchErr))
			continue
		}
		sb.WriteString(formatPortfolioBlock(tp.Snapshot, tp.Stats, lang, ""))
	}
	return sb.String()
}

// formatPortfolioFooterOneLine is a compact summary for trade alert footers.
func formatPortfolioFooterOneLine(acct AccountSnapshot) string {
	uInd := pnlDot(acct.UnrealizedProfit)
	openLabel := "open"
	if acct.PositionCount == 1 {
		openLabel = "open"
	}
	return fmt.Sprintf("Equity <b>%s</b> · uPnL %s <b>%s</b> · <b>%d</b> %s",
		formatMoney(acct.TotalEquity), uInd, formatMoney(acct.UnrealizedProfit), acct.PositionCount, openLabel)
}

// formatTradeAlertRich renders the classic order notification:
//
//	🔵 NOFX Autopilot
//	Copy trade bot: NOFX Autopilot
//	OPEN LONG  BTCUSDT
//	FILLED — 0.0100 @ $95000 · 10x
func formatTradeAlertRich(st *store.Store, e events.TradeEvent, lang string, footer *AccountSnapshot) string {
	traderName, _ := lookupTraderMeta(st, e.TraderID)
	if traderName == "" {
		traderName = strings.TrimSpace(e.TraderID)
	}
	symbol := strings.ToUpper(strings.TrimSpace(e.Symbol))
	if symbol == "" {
		symbol = e.Symbol
	}

	dot, status := "🔵", "FILLED"
	if strings.HasPrefix(e.Action, "close_") {
		dot, status = pnlDot(e.RealizedPnL), "CLOSED"
		if e.PartialClose {
			status = "PARTIAL"
		}
	}
	if lang == "zh" {
		switch status {
		case "FILLED":
			status = "已成交"
		case "CLOSED":
			status = "已平仓"
		case "PARTIAL":
			status = "部分平仓"
		}
	}

	var sb strings.Builder
	if traderName != "" {
		sb.WriteString(fmt.Sprintf("%s <b>%s</b>\n", dot, escapeHTML(traderName)))
		sb.WriteString(fmt.Sprintf("Copy trade bot: <b>%s</b>\n", escapeHTML(traderName)))
	} else {
		sb.WriteString(dot + "\n")
	}
	sb.WriteString(fmt.Sprintf("%s  %s\n", formatClassicAction(e.Action), escapeHTML(symbol)))
	fill := fmt.Sprintf("%s — %s @ %s", status, formatQty(e.Quantity), formatPrice(e.Price))
	if e.Leverage > 0 {
		fill += fmt.Sprintf(" · %.0fx", e.Leverage)
	}
	sb.WriteString(fill)
	if strings.HasPrefix(e.Action, "close_") {
		sb.WriteString(fmt.Sprintf("\nPnL: <b>%s</b>", formatPnLMoney(e.RealizedPnL)))
	}
	if footer != nil {
		sb.WriteString("\n")
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

func formatSystemAlert(st *store.Store, e events.SystemAlertEvent, lang string) string {
	name := strings.TrimSpace(e.TraderName)
	if name == "" && st != nil {
		name, _ = lookupTraderMeta(st, e.TraderID)
	}
	if name == "" {
		name = e.TraderID
	}
	title := "⚠️ System alert"
	if lang == "zh" {
		title = "⚠️ 系统告警"
	}
	switch e.Type {
	case events.AlertSafeMode:
		title = "🛡️ Safe mode ON"
		if lang == "zh" {
			title = "🛡️ 安全模式已开启"
		}
	case events.AlertSafeModeOff:
		title = "✅ Safe mode OFF"
		if lang == "zh" {
			title = "✅ 安全模式已关闭"
		}
	case events.AlertWalletEmpty:
		title = "💸 AI wallet empty"
		if lang == "zh" {
			title = "💸 AI 钱包余额不足"
		}
	case events.AlertQuotaExhausted:
		title = "🚫 AI quota exhausted"
		if lang == "zh" {
			title = "🚫 AI 配额已用尽"
		}
	case events.AlertRateLimited:
		title = "⏳ AI rate limited"
		if lang == "zh" {
			title = "⏳ AI 触发限流"
		}
	case events.AlertCopyMirror:
		title = "📋 Copy trade mirrored"
		if lang == "zh" {
			title = "📋 跟单已执行"
		}
	case events.AlertCopySkipped:
		title = "⏭️ Copy skipped"
		if lang == "zh" {
			title = "⏭️ 跟单已跳过"
		}
	case events.AlertCopyFailed:
		title = "❌ Copy failed"
		if lang == "zh" {
			title = "❌ 跟单失败"
		}
	case events.AlertCopyOverflow:
		title = "↪️ Copy overflow"
		if lang == "zh" {
			title = "↪️ 跟单溢出"
		}
	case events.AlertCopyLeaderRule:
		title = "📋 Leader copy rule"
		if lang == "zh" {
			title = "📋 领单跟单规则"
		}
	case events.AlertCopyPaused:
		title = "⏸️ Copy paused (L3)"
		if lang == "zh" {
			title = "⏸️ 跟单已暂停 (L3)"
		}
	case events.AlertCopyLossPause:
		title = "⏸️ Copy auto-paused (loss streak)"
		if lang == "zh" {
			title = "⏸️ 连亏自动暂停跟单"
		}
	case events.AlertCopyL2Evicted:
		title = "🔀 L2 evicted for L1"
		if lang == "zh" {
			title = "🔀 L1 优先关闭 L2"
		}
	case events.AlertLiquidationRisk:
		title = "🚨 Add funds — liquidation risk"
		if lang == "zh" {
			title = "🚨 请加保证金 — 接近强平"
		}
	}
	if e.Type == events.AlertLiquidationRisk {
		reason := strings.TrimSpace(e.Message)
		reason = strings.TrimPrefix(reason, name+" ")
		reason = strings.TrimSuffix(reason, ". Add funds to this wallet.")
		wallet, note := liquidationFundingDestination(name, lang)
		return fmt.Sprintf("%s\n\n┣ Bot: <b>%s</b>\n┣ Fund: <b>%s</b>\n┣ Risk: %s\n┗ %s",
			title, escapeHTML(name), escapeHTML(wallet), escapeHTML(reason), escapeHTML(note))
	}
	return fmt.Sprintf("%s\n\n┣ <b>%s</b>\n┗ %s", title, escapeHTML(name), escapeHTML(e.Message))
}

func liquidationFundingDestination(traderName, lang string) (string, string) {
	if inferVenue(traderName) == "Bitget" {
		if lang == "zh" {
			return "Bitget — Crypto BigG", "向 Crypto BigG 的 Bitget 账户添加保证金。"
		}
		return "Bitget — Crypto BigG", "Add margin to Crypto BigG on Bitget."
	}
	if inferVenue(traderName) == "Hyperliquid" {
		if lang == "zh" {
			return "Hyperliquid 交易钱包", "这是 Leviathan、Grinder、Money Printer、Copy L4 和 Alpha 6859 共用的钱包。"
		}
		return "Hyperliquid trading wallet", "Shared by Leviathan, Grinder, Money Printer, Copy L4, and Alpha 6859."
	}
	if lang == "zh" {
		return traderName + " 交易钱包", "向此机器人使用的交易钱包添加保证金。"
	}
	return traderName + " trading wallet", "Add margin to the trading wallet used by this bot."
}
