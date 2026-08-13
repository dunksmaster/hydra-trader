package telegram

import (
	"encoding/json"
	"fmt"
	"net/url"
	"nofx/store"
	"strings"
)

// TraderInfo is one row from GET /api/my-traders.
type TraderInfo struct {
	TraderID     string
	TraderName   string
	StrategyName string
	IsRunning    bool
}

// TraderPortfolio is account + positions for one trader (or an error).
type TraderPortfolio struct {
	Info      TraderInfo
	Snapshot  AccountSnapshot
	Positions []map[string]any
	FetchErr  string
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
	return tp
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

func fetchAllTraderPortfolios(c *quickClient) ([]TraderPortfolio, error) {
	all, err := fetchMyTraders(c)
	if err != nil {
		return nil, err
	}
	out := make([]TraderPortfolio, 0, len(all))
	for _, info := range all {
		out = append(out, fetchTraderPortfolio(c, info))
	}
	return out, nil
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
