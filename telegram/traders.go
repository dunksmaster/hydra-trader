package telegram

import (
	"encoding/json"
	"fmt"
	"net/url"
	"nofx/store"
	"strings"
	"sync"
)

// TraderInfo is one row from GET /api/my-traders.
type TraderInfo struct {
	TraderID     string
	TraderName   string
	StrategyName string
	Exchange     string
	IsRunning    bool
}

// TraderPortfolio is account + positions for one trader (or an error).
type TraderPortfolio struct {
	Info      TraderInfo
	Snapshot  AccountSnapshot
	Stats     TradingStats
	Positions []map[string]any
	FetchErr  string
}

// ClosedTrade is one row from GET /api/positions/history.
type ClosedTrade struct {
	Symbol      string
	Side        string
	EntryPrice  float64
	ExitPrice   float64
	Quantity    float64
	RealizedPnL float64
	Fee         float64
	Leverage    int
	ExitTime    int64
	CloseReason string
}

// TraderHistory is closed-trade history for one trader.
type TraderHistory struct {
	Info     TraderInfo
	Trades   []ClosedTrade
	Stats    TradingStats
	FetchErr string
}

// TradingStats holds closed-trade performance from GET /api/statistics/full.
type TradingStats struct {
	TotalTrades  int
	WinTrades    int
	LossTrades   int
	WinRate      float64
	TotalPnL     float64 // realized from closed positions
	TotalFee     float64
	ProfitFactor float64
}

func fetchMyTraders(c *quickClient) ([]TraderInfo, error) {
	body, err := c.get("/api/my-traders")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		TraderID     string `json:"trader_id"`
		TraderName   string `json:"trader_name"`
		StrategyName string `json:"strategy_name"`
		Exchange     string `json:"exchange"`
		IsRunning    bool   `json:"is_running"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	out := make([]TraderInfo, 0, len(raw))
	for _, t := range raw {
		out = append(out, TraderInfo{
			TraderID:     t.TraderID,
			TraderName:   t.TraderName,
			StrategyName: t.StrategyName,
			Exchange:     t.Exchange,
			IsRunning:    t.IsRunning,
		})
	}
	return out, nil
}

func resolveTraderSelection(st *store.Store, all []TraderInfo) []TraderInfo {
	if len(all) == 0 {
		return nil
	}
	sel := st.TelegramConfig().GetSelectedTraderID()
	if sel == store.SelectedTraderAll || sel == "*" {
		return all
	}
	for _, t := range all {
		if t.TraderID == sel {
			return []TraderInfo{t}
		}
	}
	// Saved id missing — fall back to first running, else first.
	for _, t := range all {
		if t.IsRunning {
			return []TraderInfo{t}
		}
	}
	return []TraderInfo{all[0]}
}

// fetchTraderPortfolioLite loads positions only (no account block or stats).
// Used by /orders for a fast Telegram response.
func fetchTraderPortfolioLite(c *quickClient, info TraderInfo) TraderPortfolio {
	tp := TraderPortfolio{Info: info}
	posBody, err := c.get("/api/positions?trader_id=" + url.QueryEscape(info.TraderID))
	if err != nil {
		tp.FetchErr = traderFetchErrMsg(err)
		return tp
	}
	var positions []map[string]any
	if err := json.Unmarshal(posBody, &positions); err != nil {
		tp.FetchErr = err.Error()
		return tp
	}
	tp.Positions = positions
	tp.Snapshot.PositionCount = len(positions)
	return tp
}

func fetchVenuePortfoliosForTelegram(c *quickClient) ([]TraderPortfolio, error) {
	all, err := fetchMyTraders(c)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}

	type venueBucket struct {
		rep  TraderInfo
		bots []TraderInfo
	}
	buckets := make(map[string]*venueBucket)
	order := make([]string, 0)
	for _, info := range all {
		key := traderExchangeKeyFromInfo(info)
		if key == "" {
			key = "unknown"
		}
		b, ok := buckets[key]
		if !ok {
			b = &venueBucket{rep: info}
			buckets[key] = b
			order = append(order, key)
		}
		b.bots = append(b.bots, info)
		if info.IsRunning && !b.rep.IsRunning {
			b.rep = info
		}
	}

	type venueFetch struct {
		key       string
		positions []map[string]any
		fetchErr  string
	}
	fetchCh := make(chan venueFetch, len(order))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2) // limit HL API pressure (429 avoidance)
	for _, key := range order {
		b := buckets[key]
		wg.Add(1)
		go func(key string, rep TraderInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			tp := fetchTraderPortfolioLite(c, rep)
			fetchCh <- venueFetch{key: key, positions: tp.Positions, fetchErr: tp.FetchErr}
		}(key, b.rep)
	}
	wg.Wait()
	close(fetchCh)

	byKey := make(map[string]venueFetch, len(order))
	for f := range fetchCh {
		byKey[f.key] = f
	}

	out := make([]TraderPortfolio, 0, len(all))
	for _, info := range all {
		key := traderExchangeKeyFromInfo(info)
		if key == "" {
			key = "unknown"
		}
		f := byKey[key]
		tp := TraderPortfolio{
			Info:      info,
			Positions: f.positions,
			FetchErr:  f.fetchErr,
		}
		tp.Snapshot.PositionCount = len(f.positions)
		out = append(out, tp)
	}
	return out, nil
}

func traderExchangeKeyFromInfo(info TraderInfo) string {
	if ex := normalizeExchangeKey(info.Exchange); ex != "" {
		return ex
	}
	return strings.ToLower(strings.TrimSpace(inferVenue(info.TraderName)))
}

func fetchAllTraderPortfoliosLite(c *quickClient) ([]TraderPortfolio, error) {
	all, err := fetchMyTraders(c)
	if err != nil {
		return nil, err
	}
	return fetchTraderPortfoliosLite(c, all), nil
}

func fetchTraderPortfoliosLite(c *quickClient, infos []TraderInfo) []TraderPortfolio {
	if len(infos) == 0 {
		return nil
	}
	out := make([]TraderPortfolio, len(infos))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, info := range infos {
		wg.Add(1)
		go func(i int, info TraderInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = fetchTraderPortfolioLite(c, info)
		}(i, info)
	}
	wg.Wait()
	return out
}

func fetchTraderPortfolio(c *quickClient, info TraderInfo) TraderPortfolio {
	tp := TraderPortfolio{Info: info}
	acctBody, err := c.get("/api/account?trader_id=" + url.QueryEscape(info.TraderID))
	if err != nil {
		tp.FetchErr = traderFetchErrMsg(err)
		return tp
	}
	var acct map[string]any
	if err := json.Unmarshal(acctBody, &acct); err != nil {
		tp.FetchErr = err.Error()
		return tp
	}
	posBody, err := c.get("/api/positions?trader_id=" + url.QueryEscape(info.TraderID))
	if err != nil {
		tp.FetchErr = traderFetchErrMsg(err)
		tp.Snapshot = ParseAccountSnapshot(acct, info.TraderName)
		tp.Snapshot.StrategyName = info.StrategyName
		return tp
	}
	var positions []map[string]any
	if err := json.Unmarshal(posBody, &positions); err != nil {
		tp.FetchErr = err.Error()
		return tp
	}
	tp.Snapshot = ParseAccountSnapshot(acct, info.TraderName)
	tp.Snapshot.StrategyName = info.StrategyName
	tp.Snapshot.PositionCount = len(positions)
	tp.Positions = positions
	tp.Stats = fetchTradingStats(c, info.TraderID)
	return tp
}

func fetchTradingStats(c *quickClient, traderID string) TradingStats {
	body, err := c.get("/api/statistics/full?trader_id=" + url.QueryEscape(traderID))
	if err != nil {
		return TradingStats{}
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return TradingStats{}
	}
	return TradingStats{
		TotalTrades:  int(jsonNum(raw["total_trades"])),
		WinTrades:    int(jsonNum(raw["win_trades"])),
		LossTrades:   int(jsonNum(raw["loss_trades"])),
		WinRate:      jsonNum(raw["win_rate"]),
		TotalPnL:     jsonNum(raw["total_pnl"]),
		TotalFee:     jsonNum(raw["total_fee"]),
		ProfitFactor: jsonNum(raw["profit_factor"]),
	}
}

func fetchPortfoliosForSelection(st *store.Store, c *quickClient) ([]TraderPortfolio, error) {
	all, err := fetchMyTraders(c)
	if err != nil {
		return nil, err
	}
	selected := resolveTraderSelection(st, all)
	out := make([]TraderPortfolio, 0, len(selected))
	for _, info := range selected {
		out = append(out, fetchTraderPortfolio(c, info))
	}
	return out, nil
}

func fetchTraderHistory(c *quickClient, info TraderInfo, limit int) TraderHistory {
	th := TraderHistory{Info: info}
	path := fmt.Sprintf("/api/positions/history?trader_id=%s&limit=%d", url.QueryEscape(info.TraderID), limit)
	body, err := c.get(path)
	if err != nil {
		th.FetchErr = traderFetchErrMsg(err)
		return th
	}
	var raw struct {
		Positions []map[string]any `json:"positions"`
		Stats     map[string]any   `json:"stats"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		th.FetchErr = err.Error()
		return th
	}
	th.Trades = parseClosedTrades(raw.Positions)
	if raw.Stats != nil {
		th.Stats = TradingStats{
			TotalTrades:  int(jsonNum(raw.Stats["total_trades"])),
			WinTrades:    int(jsonNum(raw.Stats["win_trades"])),
			LossTrades:   int(jsonNum(raw.Stats["loss_trades"])),
			WinRate:      jsonNum(raw.Stats["win_rate"]),
			TotalPnL:     jsonNum(raw.Stats["total_pnl"]),
			TotalFee:     jsonNum(raw.Stats["total_fee"]),
			ProfitFactor: jsonNum(raw.Stats["profit_factor"]),
		}
	}
	return th
}

func parseClosedTrades(rows []map[string]any) []ClosedTrade {
	out := make([]ClosedTrade, 0, len(rows))
	for _, p := range rows {
		out = append(out, ClosedTrade{
			Symbol:      posString(p, "symbol"),
			Side:        posString(p, "side"),
			EntryPrice:  posFloat(p, "entry_price"),
			ExitPrice:   posFloat(p, "exit_price"),
			Quantity:    posFloat(p, "quantity", "entry_quantity"),
			RealizedPnL: posFloat(p, "realized_pnl"),
			Fee:         posFloat(p, "fee"),
			Leverage:    int(posFloat(p, "leverage")),
			ExitTime:    int64(posFloat(p, "exit_time")),
			CloseReason: posString(p, "close_reason"),
		})
	}
	return out
}

func fetchAllTraderHistories(c *quickClient, limit int) ([]TraderHistory, error) {
	all, err := fetchMyTraders(c)
	if err != nil {
		return nil, err
	}
	out := make([]TraderHistory, 0, len(all))
	for _, info := range all {
		out = append(out, fetchTraderHistory(c, info, limit))
	}
	return out, nil
}

func fetchAllTraderPortfolios(c *quickClient) ([]TraderPortfolio, error) {
	all, err := fetchMyTraders(c)
	if err != nil {
		return nil, err
	}
	return fetchTraderPortfoliosParallel(c, all), nil
}

func fetchTraderPortfoliosParallel(c *quickClient, infos []TraderInfo) []TraderPortfolio {
	if len(infos) == 0 {
		return nil
	}
	out := make([]TraderPortfolio, len(infos))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, info := range infos {
		wg.Add(1)
		go func(i int, info TraderInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = fetchTraderPortfolio(c, info)
		}(i, info)
	}
	wg.Wait()
	return out
}

func fetchOrdersForPortfolios(c *quickClient, portfolios []TraderPortfolio, limit int) map[string][]map[string]any {
	out := make(map[string][]map[string]any)
	if limit <= 0 {
		limit = 20
	}
	if len(portfolios) == 0 {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)
	for _, tp := range portfolios {
		if tp.Info.TraderID == "" {
			continue
		}
		wg.Add(1)
		go func(tp TraderPortfolio) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			body, err := c.get(fmt.Sprintf("/api/orders?trader_id=%s&limit=%d", url.QueryEscape(tp.Info.TraderID), limit))
			if err != nil {
				return
			}
			var rows []map[string]any
			if err := json.Unmarshal(body, &rows); err != nil {
				return
			}
			mu.Lock()
			out[tp.Info.TraderID] = rows
			mu.Unlock()
		}(tp)
	}
	wg.Wait()
	return out
}

func fetchRunningTraderPortfolios(c *quickClient) ([]TraderPortfolio, error) {
	all, err := fetchMyTraders(c)
	if err != nil {
		return nil, err
	}
	out := make([]TraderPortfolio, 0)
	for _, info := range all {
		if !info.IsRunning {
			continue
		}
		out = append(out, fetchTraderPortfolio(c, info))
	}
	return out, nil
}

func traderFetchErrMsg(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "404") || strings.Contains(strings.ToLower(msg), "not found") {
		return "trader not loaded (start it in the dashboard)"
	}
	return msg
}

func lookupTraderMeta(st *store.Store, traderID string) (name, strategy string) {
	if traderID == "" {
		return "", ""
	}
	traders, err := st.Trader().ListAll()
	if err != nil {
		return "", ""
	}
	for _, t := range traders {
		if t.ID != traderID {
			continue
		}
		name = t.Name
		if t.StrategyID != "" {
			if strat, err := st.Strategy().Get(t.UserID, t.StrategyID); err == nil {
				strategy = strat.Name
			}
		}
		return name, strategy
	}
	if len(traderID) >= 8 {
		return traderID[:8], ""
	}
	return traderID, ""
}

func fetchTraderPortfolioByID(st *store.Store, c *quickClient, traderID string) (TraderPortfolio, error) {
	all, err := fetchMyTraders(c)
	if err != nil {
		return TraderPortfolio{}, err
	}
	for _, info := range all {
		if info.TraderID == traderID {
			return fetchTraderPortfolio(c, info), nil
		}
	}
	return TraderPortfolio{}, fmt.Errorf("trader not found")
}
