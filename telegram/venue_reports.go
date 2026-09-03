package telegram

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const (
	venueHistoryTradeLimit = 5
	venueOrdersFillLimit   = 8
)

// VenueHistorySummary is closed-trade data merged per exchange wallet.
type VenueHistorySummary struct {
	Exchange string
	Label    string
	Trades   []ClosedTrade
	Stats    TradingStats
	FetchErr string
}

func aggregateHistoriesByVenue(histories []TraderHistory) []VenueHistorySummary {
	type acc struct {
		exchange string
		label    string
		trades   []ClosedTrade
		stats    TradingStats
		fetchErr string
	}
	byEx := map[string]*acc{}
	order := make([]string, 0)

	for _, th := range histories {
		ex := normalizeExchangeKey(th.Info.Exchange)
		if ex == "" {
			ex = strings.ToLower(strings.TrimSpace(inferVenue(th.Info.TraderName)))
		}
		if ex == "" {
			ex = "unknown"
		}
		a, ok := byEx[ex]
		if !ok {
			a = &acc{exchange: ex, label: venueLabel(ex)}
			byEx[ex] = a
			order = append(order, ex)
		}
		if th.FetchErr != "" && a.fetchErr == "" {
			a.fetchErr = th.FetchErr
		}
		a.trades = append(a.trades, th.Trades...)
		a.stats.TotalTrades += th.Stats.TotalTrades
		a.stats.WinTrades += th.Stats.WinTrades
		a.stats.LossTrades += th.Stats.LossTrades
		a.stats.TotalPnL += th.Stats.TotalPnL
		a.stats.TotalFee += th.Stats.TotalFee
	}
	out := make([]VenueHistorySummary, 0, len(order))
	for _, ex := range order {
		a := byEx[ex]
		if a.stats.TotalTrades > 0 {
			a.stats.WinRate = float64(a.stats.WinTrades) / float64(a.stats.TotalTrades) * 100
		}
		sort.Slice(a.trades, func(i, j int) bool { return a.trades[i].ExitTime > a.trades[j].ExitTime })
		if len(a.trades) > venueHistoryTradeLimit*3 {
			a.trades = a.trades[:venueHistoryTradeLimit*3]
		}
		out = append(out, VenueHistorySummary{
			Exchange: a.exchange,
			Label:    a.label,
			Trades:   a.trades,
			Stats:    a.stats,
			FetchErr: a.fetchErr,
		})
	}
	return out
}

func formatVenueHistoryReport(histories []TraderHistory, lang string, lossesOnly bool) string {
	title := "📜 Closed trades"
	if lossesOnly {
		if lang == "zh" {
			title = "📜 亏损交易"
		} else {
			title = "📜 Losing trades"
		}
	} else if lang == "zh" {
		title = "📜 已平仓"
	}
	venues := aggregateHistoriesByVenue(histories)
	if len(venues) == 0 {
		if lang == "zh" {
			return "<b>" + title + "</b>\n\n暂无记录。"
		}
		return "<b>" + title + "</b>\n\nNo closed trades yet."
	}

	var sections []string
	for _, v := range venues {
		if sec := formatVenueHistorySection(v, lang, lossesOnly); sec != "" {
			sections = append(sections, sec)
		}
	}
	if len(sections) == 0 {
		if lang == "zh" {
			return "<b>" + title + "</b>\n\n暂无记录。"
		}
		return "<b>" + title + "</b>\n\nNo closed trades yet."
	}
	hint := "HL + Bitget history merged by wallet · /orders for fills"
	if lang == "zh" {
		hint = "按钱包汇总 HL + Bitget · /orders 查看成交"
	}
	return "<b>" + title + "</b>\n\n" + strings.Join(sections, "\n\n────────────\n\n") + "\n\n<i>" + hint + "</i>"
}

func formatVenueHistorySection(v VenueHistorySummary, lang string, lossesOnly bool) string {
	if v.FetchErr != "" && len(v.Trades) == 0 && v.Stats.TotalTrades == 0 {
		return fmt.Sprintf("🤖 <b>%s</b>\n⚠️ %s", escapeHTML(v.Label), escapeHTML(v.FetchErr))
	}

	trades := v.Trades
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
	sb.WriteString(fmt.Sprintf("🤖 <b>%s</b>", escapeHTML(v.Label)))
	if v.Stats.TotalTrades > 0 {
		ind := pnlIndicator(v.Stats.TotalPnL)
		if lang == "zh" {
			sb.WriteString(fmt.Sprintf("\n已平 %d · %d胜/%d负 · 净 %s <b>%s</b>",
				v.Stats.TotalTrades, v.Stats.WinTrades, v.Stats.LossTrades, ind, formatMoney(v.Stats.TotalPnL)))
		} else {
			sb.WriteString(fmt.Sprintf("\nClosed %d · %dW/%dL · net %s <b>%s</b>",
				v.Stats.TotalTrades, v.Stats.WinTrades, v.Stats.LossTrades, ind, formatMoney(v.Stats.TotalPnL)))
		}
	} else if len(trades) == 0 {
		if lang == "zh" {
			sb.WriteString("\n暂无已平仓。")
		} else {
			sb.WriteString("\nNo closed trades.")
		}
		return sb.String()
	}

	if len(trades) == 0 {
		if lossesOnly {
			if lang == "zh" {
				sb.WriteString("\n✅ 无亏损。")
			} else {
				sb.WriteString("\n✅ No losses.")
			}
		}
		return sb.String()
	}

	sb.WriteString("\n")
	shown := 0
	for _, t := range trades {
		if shown >= venueHistoryTradeLimit {
			break
		}
		if shown > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(formatClosedTradeCompact(t, lang))
		shown++
	}
	if len(trades) > venueHistoryTradeLimit {
		if lang == "zh" {
			sb.WriteString(fmt.Sprintf("\n<i>…+%d 笔更早</i>", len(trades)-venueHistoryTradeLimit))
		} else {
			sb.WriteString(fmt.Sprintf("\n<i>…+%d older</i>", len(trades)-venueHistoryTradeLimit))
		}
	}
	return sb.String()
}

func formatClosedTradeCompact(t ClosedTrade, lang string) string {
	net := closedTradeNet(t)
	ind := pnlIndicator(net)
	symbol := displaySymbol(t.Symbol)
	side := strings.ToUpper(strings.TrimSpace(t.Side))
	return fmt.Sprintf("%s <b>%s %s</b> %s · %s → %s · %s",
		ind, escapeHTML(symbol), formatSideBadge(side), formatPnLMoney(net),
		formatCompactPrice(t.EntryPrice), formatCompactPrice(t.ExitPrice),
		formatExitTime(t.ExitTime, lang))
}

func formatVenuePnLReport(portfolios []TraderPortfolio, lang string) string {
	title := "📊 P&amp;L summary"
	if lang == "zh" {
		title = "📊 盈亏摘要"
	}
	if len(portfolios) == 0 {
		if lang == "zh" {
			return "<b>" + title + "</b>\n\n暂无交易员。"
		}
		return "<b>" + title + "</b>\n\nNo traders configured."
	}

	type venueAcc struct {
		label    string
		snapshot AccountSnapshot
		stats    TradingStats
		hasSnap  bool
		hasStats bool
		fetchErr string
	}
	byEx := map[string]*venueAcc{}
	order := make([]string, 0)

	for _, tp := range portfolios {
		ex := traderExchangeKey(tp)
		if ex == "" {
			ex = "unknown"
		}
		a, ok := byEx[ex]
		if !ok {
			a = &venueAcc{label: venueLabel(ex)}
			byEx[ex] = a
			order = append(order, ex)
		}
		if tp.FetchErr != "" && a.fetchErr == "" {
			a.fetchErr = tp.FetchErr
		}
		if tp.FetchErr == "" && !a.hasSnap {
			a.snapshot = tp.Snapshot
			a.hasSnap = true
		}
		if tp.Stats.TotalTrades > 0 || tp.Stats.TotalPnL != 0 {
			a.stats.TotalTrades += tp.Stats.TotalTrades
			a.stats.WinTrades += tp.Stats.WinTrades
			a.stats.LossTrades += tp.Stats.LossTrades
			a.stats.TotalPnL += tp.Stats.TotalPnL
			a.stats.TotalFee += tp.Stats.TotalFee
			a.hasStats = true
		}
	}

	var sections []string
	for _, ex := range order {
		a := byEx[ex]
		if a.fetchErr != "" && !a.hasSnap && !a.hasStats {
			sections = append(sections, fmt.Sprintf("🤖 <b>%s</b>\n⚠️ %s", escapeHTML(a.label), escapeHTML(a.fetchErr)))
			continue
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("🤖 <b>%s</b>", escapeHTML(a.label)))
		if a.hasSnap {
			uInd := pnlIndicator(a.snapshot.UnrealizedProfit)
			if lang == "zh" {
				sb.WriteString(fmt.Sprintf("\n权益 <b>%s</b> · 浮盈 %s <b>%s</b> · %d 持仓",
					formatMoney(a.snapshot.TotalEquity), uInd, formatMoney(a.snapshot.UnrealizedProfit), a.snapshot.PositionCount))
			} else {
				sb.WriteString(fmt.Sprintf("\nEquity <b>%s</b> · open %s <b>%s</b> · %d pos",
					formatMoney(a.snapshot.TotalEquity), uInd, formatMoney(a.snapshot.UnrealizedProfit), a.snapshot.PositionCount))
			}
		}
		if a.hasStats {
			if a.stats.TotalTrades > 0 {
				a.stats.WinRate = float64(a.stats.WinTrades) / float64(a.stats.TotalTrades) * 100
			}
			cInd := pnlIndicator(a.stats.TotalPnL)
			if lang == "zh" {
				sb.WriteString(fmt.Sprintf("\n已平 %d · 胜率 %.0f%% · 净 %s <b>%s</b>",
					a.stats.TotalTrades, a.stats.WinRate, cInd, formatMoney(a.stats.TotalPnL)))
			} else {
				sb.WriteString(fmt.Sprintf("\nClosed %d · %.0f%% win · net %s <b>%s</b>",
					a.stats.TotalTrades, a.stats.WinRate, cInd, formatMoney(a.stats.TotalPnL)))
			}
		} else if !a.hasSnap {
			if lang == "zh" {
				sb.WriteString("\n暂无数据。")
			} else {
				sb.WriteString("\nNo data yet.")
			}
		}
		sections = append(sections, sb.String())
	}

	hint := "/history · /orders · /positions"
	if lang == "zh" {
		hint = "/history · /orders · /positions"
	}
	return "<b>" + title + "</b>\n\n" + strings.Join(sections, "\n\n────────────\n\n") + "\n\n<i>" + hint + "</i>"
}

func fetchVenueOrdersMerged(c *quickClient, portfolios []TraderPortfolio, perVenueLimit int) map[string][]map[string]any {
	type row struct {
		exchange string
		row      map[string]any
		time     int64
	}
	var all []row
	for _, tp := range portfolios {
		if tp.Info.TraderID == "" {
			continue
		}
		body, err := c.get("/api/orders?trader_id=" + url.QueryEscape(tp.Info.TraderID) + fmt.Sprintf("&limit=%d", perVenueLimit))
		if err != nil {
			continue
		}
		var rows []map[string]any
		if json.Unmarshal(body, &rows) != nil {
			continue
		}
		ex := traderExchangeKey(tp)
		for _, r := range rows {
			all = append(all, row{
				exchange: ex,
				row:      r,
				time:     orderRowTimeMs(r),
			})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].time > all[j].time })

	out := map[string][]map[string]any{}
	for _, r := range all {
		if len(out[r.exchange]) >= perVenueLimit {
			continue
		}
		out[r.exchange] = append(out[r.exchange], r.row)
	}
	return out
}

func orderRowTimeMs(row map[string]any) int64 {
	for _, k := range []string{"updated_at", "created_at", "filled_at", "timestamp"} {
		if v := int64(posFloat(row, k)); v > 0 {
			if v < 1e12 {
				return v * 1000
			}
			return v
		}
	}
	return 0
}

func formatVenueOrdersReport(portfolios []TraderPortfolio, ordersByVenue map[string][]map[string]any, lang string) string {
	title := "📋 Orders"
	if lang == "zh" {
		title = "📋 订单"
	}
	groups := groupPositionsByVenue(portfolios)
	var sections []string

	for _, g := range groups {
		rows := ordersByVenue[g.Exchange]
		if len(g.Positions) == 0 && len(rows) == 0 {
			continue
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("🤖 <b>%s</b>", escapeHTML(g.Label)))
		if len(g.Positions) > 0 {
			if lang == "zh" {
				sb.WriteString("\n📈 持仓")
			} else {
				sb.WriteString("\n📈 Open")
			}
			for _, vp := range g.Positions {
				sb.WriteString("\n")
				sb.WriteString(formatPositionListLine(vp.Raw, lang))
			}
		}
		if len(rows) > 0 {
			if lang == "zh" {
				sb.WriteString("\n📜 成交")
			} else {
				sb.WriteString("\n📜 Fills")
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
				if shown >= venueOrdersFillLimit {
					break
				}
			}
		}
		if len(g.Positions) == 0 && len(rows) == 0 {
			continue
		}
		sections = append(sections, sb.String())
	}

	if len(sections) == 0 {
		if lang == "zh" {
			return "<b>" + title + "</b>\n\n暂无订单。"
		}
		return "<b>" + title + "</b>\n\nNo orders yet."
	}
	return "<b>" + title + "</b>\n\n" + strings.Join(sections, "\n\n────────────\n\n")
}

func formatAccountSummaryReport(portfolios []TraderPortfolio, histories []TraderHistory, lang string) string {
	title := "📋 Account summary"
	if lang == "zh" {
		title = "📋 账户摘要"
	}
	pnl := formatVenuePnLReport(portfolios, lang)
	hist := formatVenueHistoryReport(histories, lang, false)
	// Strip duplicate titles from sub-reports
	pnl = strings.TrimPrefix(pnl, "<b>📊 P&amp;L summary</b>\n\n")
	pnl = strings.TrimPrefix(pnl, "<b>📊 盈亏摘要</b>\n\n")
	hist = strings.TrimPrefix(hist, "<b>📜 Closed trades</b>\n\n")
	hist = strings.TrimPrefix(hist, "<b>📜 已平仓</b>\n\n")
	if i := strings.LastIndex(hist, "\n\n<i>"); i >= 0 {
		hist = hist[:i]
	}
	return "<b>" + title + "</b>\n\n" + pnl + "\n\n────────────\n\n" + hist
}
