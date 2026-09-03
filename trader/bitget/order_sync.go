package bitget

import (
	"encoding/json"
	"fmt"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"nofx/trader/syncloop"
	"sort"
	"strconv"
	"strings"
	"time"
)

type bitgetFill struct {
	TradeID    string `json:"tradeId"`
	Symbol     string `json:"symbol"`
	OrderID    string `json:"orderId"`
	Side       string `json:"side"`
	Price      string `json:"price"`
	BaseVolume string `json:"baseVolume"`
	Profit     string `json:"profit"`
	CTime      string `json:"cTime"`
	TradeSide  string `json:"tradeSide"`
	FeeDetail  []struct {
		FeeCoin  string `json:"feeCoin"`
		TotalFee string `json:"totalFee"`
	} `json:"feeDetail"`
}

// parseBitgetFillHistory accepts Bitget's wrapped object ({"fillList":[...],"endId":...})
// including a null fillList (no fills yet), or a bare JSON array.
func parseBitgetFillHistory(data []byte) ([]bitgetFill, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err == nil {
		if raw, ok := obj["fillList"]; ok {
			if len(raw) == 0 || string(raw) == "null" {
				return []bitgetFill{}, nil
			}
			var fills []bitgetFill
			if err := json.Unmarshal(raw, &fills); err != nil {
				return nil, err
			}
			return fills, nil
		}
	}
	var fills []bitgetFill
	if err := json.Unmarshal(data, &fills); err != nil {
		return nil, err
	}
	return fills, nil
}

// BitgetTrade represents a trade record from Bitget fill history
type BitgetTrade struct {
	Symbol      string
	TradeID     string
	OrderID     string
	Side        string // buy or sell
	FillPrice   float64
	FillQty     float64
	Fee         float64
	FeeAsset    string
	ExecTime    time.Time
	ProfitLoss  float64
	OrderType   string
	OrderAction string // open_long, open_short, close_long, close_short
}

// GetTrades retrieves trade/fill records from Bitget
func (t *BitgetTrader) GetTrades(startTime time.Time, limit int) ([]BitgetTrade, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100 // Bitget max limit is 100
	}
	if t.useUTA() {
		return t.utaGetTrades(startTime, limit)
	}

	params := map[string]interface{}{
		"productType": "USDT-FUTURES",
		"startTime":   fmt.Sprintf("%d", startTime.UnixMilli()),
		"limit":       fmt.Sprintf("%d", limit),
	}

	data, err := t.doRequest("GET", "/api/v2/mix/order/fill-history", params)
	if err != nil {
		if isBitgetClassicBlocked(err) {
			return t.utaGetTrades(startTime, limit)
		}
		return nil, fmt.Errorf("failed to get fill history: %w", err)
	}

	directFills, err := parseBitgetFillHistory(data)
	if err != nil {
		logger.Infof("⚠️ Bitget fill-history parse failed, raw: %s", string(data))
		return nil, fmt.Errorf("failed to parse fills: %w", err)
	}
	logger.Infof("🔍 Bitget: parsed fill-history, fills count: %d", len(directFills))

	trades := make([]BitgetTrade, 0, len(directFills))

	for _, fill := range directFills {
		fillPrice, _ := strconv.ParseFloat(fill.Price, 64)
		fillQty, _ := strconv.ParseFloat(fill.BaseVolume, 64)
		profit, _ := strconv.ParseFloat(fill.Profit, 64)
		cTime, _ := strconv.ParseInt(fill.CTime, 10, 64)

		// Extract fee from feeDetail array (Bitget V2 API)
		var fee float64
		var feeAsset string
		if len(fill.FeeDetail) > 0 {
			fee, _ = strconv.ParseFloat(fill.FeeDetail[0].TotalFee, 64)
			feeAsset = fill.FeeDetail[0].FeeCoin
		}

		// Determine order action based on side and tradeSide
		// Bitget one-way mode: buy_single (open long), sell_single (close long)
		// Bitget hedge mode: open + buy = open_long, close + sell = close_long
		orderAction := "open_long"
		side := strings.ToLower(fill.Side)
		tradeSide := strings.ToLower(fill.TradeSide)

		// One-way position mode (buy_single/sell_single)
		if tradeSide == "buy_single" {
			orderAction = "open_long"
		} else if tradeSide == "sell_single" {
			orderAction = "close_long"
		} else if tradeSide == "open" {
			// Hedge mode: open
			if side == "buy" {
				orderAction = "open_long"
			} else {
				orderAction = "open_short"
			}
		} else if tradeSide == "close" {
			// Hedge mode: close
			if side == "sell" {
				orderAction = "close_long"
			} else {
				orderAction = "close_short"
			}
		}

		trade := BitgetTrade{
			Symbol:      fill.Symbol,
			TradeID:     fill.TradeID,
			OrderID:     fill.OrderID,
			Side:        fill.Side,
			FillPrice:   fillPrice,
			FillQty:     fillQty,
			Fee:         -fee, // Bitget returns negative fee, convert to positive
			FeeAsset:    feeAsset,
			ExecTime:    time.UnixMilli(cTime).UTC(),
			ProfitLoss:  profit,
			OrderType:   "MARKET",
			OrderAction: orderAction,
		}

		trades = append(trades, trade)
	}

	return trades, nil
}

// SyncOrdersFromBitget syncs Bitget exchange order history to local database
// Also creates/updates position records to ensure orders/fills/positions data consistency
// exchangeID: Exchange account UUID (from exchanges.id)
// exchangeType: Exchange type ("bitget")
func (t *BitgetTrader) SyncOrdersFromBitget(traderID string, exchangeID string, exchangeType string, st *store.Store) error {
	if st == nil {
		return fmt.Errorf("store is nil")
	}

	// Get recent trades (last 24 hours)
	startTime := time.Now().Add(-24 * time.Hour)

	logger.Infof("🔄 Syncing Bitget trades from: %s", startTime.Format(time.RFC3339))

	// Use GetTrades method to fetch trade records
	trades, err := t.GetTrades(startTime, 100)
	if err != nil {
		return fmt.Errorf("failed to get trades: %w", err)
	}

	logger.Infof("📥 Received %d trades from Bitget", len(trades))

	// Sort trades by time ASC (oldest first) for proper position building
	sort.Slice(trades, func(i, j int) bool {
		return trades[i].ExecTime.UnixMilli() < trades[j].ExecTime.UnixMilli()
	})

	// Process trades one by one (no transaction to avoid deadlock)
	orderStore := st.Order()
	positionStore := st.Position()
	posBuilder := store.NewPositionBuilder(positionStore)
	syncedCount := 0

	for _, trade := range trades {
		// Check if trade already exists (use exchangeID which is UUID, not exchange type)
		existing, err := orderStore.GetOrderByExchangeID(exchangeID, trade.TradeID)
		if err == nil && existing != nil {
			continue // Order already exists, skip
		}

		// Normalize symbol
		symbol := market.Normalize(trade.Symbol)

		orderAction := reconcileBitgetOrderAction(traderID, positionStore, symbol, trade.Side, trade.OrderAction)

		// Determine position side from order action
		positionSide := "LONG"
		if strings.Contains(orderAction, "short") {
			positionSide = "SHORT"
		}

		// Normalize side for storage
		side := strings.ToUpper(trade.Side)

		// Create order record - use UTC time in milliseconds to avoid timezone issues
		execTimeMs := trade.ExecTime.UTC().UnixMilli()
		orderRecord := &store.TraderOrder{
			TraderID:        traderID,
			ExchangeID:      exchangeID,   // UUID
			ExchangeType:    exchangeType, // Exchange type
			ExchangeOrderID: trade.TradeID,
			Symbol:          symbol,
			Side:            side,
			PositionSide:    "BOTH", // Bitget uses one-way position mode
			Type:            trade.OrderType,
			OrderAction:     orderAction,
			Quantity:        trade.FillQty,
			Price:           trade.FillPrice,
			Status:          "FILLED",
			FilledQuantity:  trade.FillQty,
			AvgFillPrice:    trade.FillPrice,
			Commission:      trade.Fee,
			FilledAt:        execTimeMs,
			CreatedAt:       execTimeMs,
			UpdatedAt:       execTimeMs,
		}

		// Insert order record
		if err := orderStore.CreateOrder(orderRecord); err != nil {
			logger.Infof("  ⚠️ Failed to sync trade %s: %v", trade.TradeID, err)
			continue
		}

		// Create fill record - use UTC time in milliseconds
		fillRecord := &store.TraderFill{
			TraderID:        traderID,
			ExchangeID:      exchangeID,   // UUID
			ExchangeType:    exchangeType, // Exchange type
			OrderID:         orderRecord.ID,
			ExchangeOrderID: trade.OrderID,
			ExchangeTradeID: trade.TradeID,
			Symbol:          symbol,
			Side:            side,
			Price:           trade.FillPrice,
			Quantity:        trade.FillQty,
			QuoteQuantity:   trade.FillPrice * trade.FillQty,
			Commission:      trade.Fee,
			CommissionAsset: trade.FeeAsset,
			RealizedPnL:     trade.ProfitLoss,
			IsMaker:         false,
			CreatedAt:       execTimeMs,
		}

		if err := orderStore.CreateFill(fillRecord); err != nil {
			logger.Infof("  ⚠️ Failed to sync fill for trade %s: %v", trade.TradeID, err)
		}

		// Create/update position record using PositionBuilder
		if err := posBuilder.ProcessTrade(
			traderID, exchangeID, exchangeType,
			symbol, positionSide, orderAction,
			trade.FillQty, trade.FillPrice, trade.Fee, trade.ProfitLoss,
			execTimeMs, trade.TradeID,
		); err != nil {
			logger.Infof("  ⚠️ Failed to sync position for trade %s: %v", trade.TradeID, err)
		} else {
			logger.Infof("  📍 Position updated for trade: %s (action: %s, qty: %.6f)", trade.TradeID, orderAction, trade.FillQty)
		}

		syncedCount++
		logger.Infof("  ✅ Synced trade: %s %s %s qty=%.6f price=%.6f pnl=%.2f fee=%.6f action=%s",
			trade.TradeID, symbol, side, trade.FillQty, trade.FillPrice, trade.ProfitLoss, trade.Fee, orderAction)
	}

	logger.Infof("✅ Bitget order sync completed: %d new trades synced", syncedCount)
	return nil
}

// reconcileBitgetOrderAction fixes UTA fills that omit execPnl/tradeSide on closes
// (sell with open long was mislabeled open_short → wrong Telegram alert).
func reconcileBitgetOrderAction(traderID string, positionStore openPositionLookup, symbol, side, orderAction string) string {
	if positionStore == nil || !strings.HasPrefix(orderAction, "open_") {
		return orderAction
	}
	side = strings.ToLower(strings.TrimSpace(side))
	switch side {
	case "sell":
		if open, err := positionStore.GetOpenPositionBySymbol(traderID, symbol, "LONG"); err == nil && open != nil {
			return "close_long"
		}
	case "buy":
		if open, err := positionStore.GetOpenPositionBySymbol(traderID, symbol, "SHORT"); err == nil && open != nil {
			return "close_short"
		}
	}
	return orderAction
}

type openPositionLookup interface {
	GetOpenPositionBySymbol(traderID, symbol, side string) (*store.TraderPosition, error)
}

// StartOrderSync starts background order sync task for Bitget
func (t *BitgetTrader) StartOrderSync(traderID string, exchangeID string, exchangeType string, st *store.Store, interval time.Duration, stop <-chan struct{}) {
	syncloop.Run(stop, interval, "Bitget", func() error {
		return t.SyncOrdersFromBitget(traderID, exchangeID, exchangeType, st)
	})
}
