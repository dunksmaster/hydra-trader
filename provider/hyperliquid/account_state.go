package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// AccountPosition is a normalized open perp leg from clearinghouseState.
type AccountPosition struct {
	Coin        string
	Symbol      string
	Side        string // "long" or "short"
	Size        float64
	NotionalUSD float64
	Leverage    int
	EntryPrice  float64
}

// AccountState summarizes a Hyperliquid wallet's perp book.
type AccountState struct {
	Address string
	Equity  float64
	Legs    []AccountPosition
}

type clearinghouseStateResponse struct {
	MarginSummary struct {
		AccountValue    string `json:"accountValue"`
		TotalMarginUsed string `json:"totalMarginUsed"`
	} `json:"marginSummary"`
	CrossMarginSummary struct {
		AccountValue    string `json:"accountValue"`
		TotalMarginUsed string `json:"totalMarginUsed"`
	} `json:"crossMarginSummary"`
	AssetPositions []struct {
		Position struct {
			Coin          string  `json:"coin"`
			Szi           string  `json:"szi"`
			EntryPx       *string `json:"entryPx"`
			PositionValue string  `json:"positionValue"`
			Leverage      struct {
				Value int `json:"value"`
			} `json:"leverage"`
		} `json:"position"`
	} `json:"assetPositions"`
}

var defaultInfoClient = &http.Client{Timeout: 30 * time.Second}

// PostInfo posts a payload to the Hyperliquid info endpoint.
func PostInfo(ctx context.Context, payload map[string]any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hyperliquidInfoURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := defaultInfoClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hyperliquid info status %d: %s", resp.StatusCode, string(respBody))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// FetchAccountState loads main-dex perp positions and equity for a Hyperliquid wallet.
func FetchAccountState(ctx context.Context, address string) (*AccountState, error) {
	return FetchAccountStateForDex(ctx, address, "")
}

// FetchAccountStateForDex loads perp positions and equity for a specific dex ("" = main, "xyz" = hip-3).
func FetchAccountStateForDex(ctx context.Context, address, dex string) (*AccountState, error) {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return nil, fmt.Errorf("leader address is required")
	}

	payload := map[string]any{
		"type": "clearinghouseState",
		"user": address,
	}
	dex = strings.TrimSpace(dex)
	if dex != "" {
		payload["dex"] = dex
	}

	var state clearinghouseStateResponse
	if err := PostInfo(ctx, payload, &state); err != nil {
		return nil, err
	}

	return parseClearinghouseState(address, state), nil
}

// FetchAccountStateAll merges main and xyz dex legs for copy trading reconciliation.
func FetchAccountStateAll(ctx context.Context, address string) (*AccountState, error) {
	main, err := FetchAccountStateForDex(ctx, address, "")
	if err != nil {
		return nil, err
	}
	xyz, err := FetchAccountStateForDex(ctx, address, "xyz")
	if err != nil {
		// xyz book may be empty for wallets that never traded hip-3; keep main only.
		return main, nil
	}
	return mergeAccountStates(main, xyz), nil
}

func parseClearinghouseState(address string, state clearinghouseStateResponse) *AccountState {
	equity := parseHLFloat(state.MarginSummary.AccountValue)
	if equity == 0 {
		equity = parseHLFloat(state.CrossMarginSummary.AccountValue)
	}

	out := &AccountState{
		Address: address,
		Equity:  equity,
		Legs:    make([]AccountPosition, 0, len(state.AssetPositions)),
	}

	for _, assetPos := range state.AssetPositions {
		size, _ := strconv.ParseFloat(assetPos.Position.Szi, 64)
		if size == 0 {
			continue
		}
		coin := strings.TrimSpace(assetPos.Position.Coin)
		notional, _ := strconv.ParseFloat(assetPos.Position.PositionValue, 64)
		if notional < 0 {
			notional = -notional
		}
		entry := 0.0
		if assetPos.Position.EntryPx != nil {
			entry, _ = strconv.ParseFloat(*assetPos.Position.EntryPx, 64)
		}
		side := "long"
		if size < 0 {
			side = "short"
			size = -size
		}
		symbol := coinToSymbol(coin)
		out.Legs = append(out.Legs, AccountPosition{
			Coin:        coin,
			Symbol:      symbol,
			Side:        side,
			Size:        size,
			NotionalUSD: notional,
			Leverage:    assetPos.Position.Leverage.Value,
			EntryPrice:  entry,
		})
	}

	return out
}

func mergeAccountStates(main, xyz *AccountState) *AccountState {
	if main == nil {
		return xyz
	}
	if xyz == nil {
		return main
	}
	out := &AccountState{
		Address: main.Address,
		Equity:  main.Equity + xyz.Equity,
		Legs:    make([]AccountPosition, 0, len(main.Legs)+len(xyz.Legs)),
	}
	seen := make(map[string]struct{}, len(main.Legs)+len(xyz.Legs))
	appendLeg := func(leg AccountPosition) {
		key := strings.ToLower(leg.Coin) + "|" + leg.Side
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out.Legs = append(out.Legs, leg)
	}
	for _, leg := range main.Legs {
		appendLeg(leg)
	}
	for _, leg := range xyz.Legs {
		appendLeg(leg)
	}
	return out
}

func coinToSymbol(coin string) string {
	coin = strings.TrimSpace(coin)
	if coin == "" {
		return ""
	}
	upper := strings.ToUpper(coin)
	if strings.HasPrefix(upper, "XYZ:") {
		return upper
	}
	// HIP-3 deployer perps keep the @ prefix; do not append USDT.
	if strings.HasPrefix(coin, "@") {
		return coin
	}
	if strings.HasSuffix(upper, "USDT") {
		return upper
	}
	return upper + "USDT"
}

func parseHLFloat(raw string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return v
}
