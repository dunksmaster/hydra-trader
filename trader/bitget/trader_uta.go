package bitget

import (
	"encoding/json"
	"fmt"
	"math"
	"nofx/logger"
	"nofx/trader/types"
	"strconv"
	"strings"
	"time"
)

const (
	utaAssetsPath            = "/api/v3/account/assets"
	utaSetLeveragePath       = "/api/v3/account/set-leverage"
	utaCurrentPositionPath   = "/api/v3/position/current-position"
	utaHistoryPositionPath   = "/api/v3/position/history-position"
	utaPlaceOrderPath        = "/api/v3/trade/place-order"
	utaCancelOrderPath       = "/api/v3/trade/cancel-order"
	utaCancelSymbolOrderPath = "/api/v3/trade/cancel-symbol-order"
	utaUnfilledOrdersPath    = "/api/v3/trade/unfilled-orders"
	utaOrderInfoPath         = "/api/v3/trade/order-info"
	utaFillsPath             = "/api/v3/trade/fills"
	utaPlaceStrategyPath     = "/api/v3/trade/place-strategy-order"
	utaCancelStrategyPath    = "/api/v3/trade/cancel-strategy-order"
	utaUnfilledStrategyPath  = "/api/v3/trade/unfilled-strategy-orders"
	utaFuturesCategory       = "USDT-FUTURES"
)

func isBitgetClassicBlocked(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "code=40085") {
		return true
	}
	if strings.Contains(s, "unified account") &&
		(strings.Contains(s, "not available") || strings.Contains(s, "please use") || strings.Contains(s, "v3")) {
		return true
	}
	return false
}

func (t *BitgetTrader) useUTA() bool {
	t.accountMu.Lock()
	defer t.accountMu.Unlock()
	return t.uta
}

func (t *BitgetTrader) markUTA() {
	t.accountMu.Lock()
	defer t.accountMu.Unlock()
	if !t.uta {
		logger.Infof("  ✓ Bitget API key is Unified Trading Account; switching private calls to v3")
	}
	t.uta = true
	t.utaKnown = true
}

func (t *BitgetTrader) markClassic() {
	t.accountMu.Lock()
	defer t.accountMu.Unlock()
	if !t.utaKnown {
		t.uta = false
		t.utaKnown = true
	}
}

func (t *BitgetTrader) rememberUTAMarginMode(symbol, marginMode string) {
	if marginMode != "isolated" {
		marginMode = "crossed"
	}
	t.accountMu.Lock()
	defer t.accountMu.Unlock()
	if t.utaMarginModes == nil {
		t.utaMarginModes = make(map[string]string)
	}
	t.utaMarginModes[t.convertSymbol(symbol)] = marginMode
}

func (t *BitgetTrader) utaMarginMode(symbol string) string {
	t.accountMu.Lock()
	defer t.accountMu.Unlock()
	if mode := t.utaMarginModes[t.convertSymbol(symbol)]; mode == "isolated" {
		return mode
	}
	return "crossed"
}

func isBitgetUTAManageDenied(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "code=40014") ||
		(strings.Contains(s, "uta manage") && strings.Contains(s, "permission"))
}

func (t *BitgetTrader) detectAccountMode() {
	data, err := t.doRequest("GET", "/api/v3/account/settings", nil)
	if err == nil {
		var info struct {
			Permissions []string `json:"permissions"`
		}
		if json.Unmarshal(data, &info) == nil {
			for _, perm := range info.Permissions {
				p := strings.ToLower(perm)
				if p == "uta_trade" || p == "uta_mgt" {
					t.markUTA()
					return
				}
			}
		}
	}
	assets, assetsErr := t.doRequest("GET", utaAssetsPath, nil)
	if assetsErr == nil && len(assets) > 0 && string(assets) != "null" {
		t.markUTA()
		return
	}
	if isBitgetUTAManageDenied(assetsErr) {
		t.markUTA()
		return
	}
	t.markClassic()
}

func (t *BitgetTrader) utaGetBalance() (map[string]interface{}, error) {
	data, err := t.doRequest("GET", utaAssetsPath, nil)
	if err != nil {
		if isBitgetUTAManageDenied(err) {
			return t.utaBalanceFromPositions()
		}
		return nil, fmt.Errorf("failed to get account balance: %w", err)
	}

	var acc struct {
		AccountEquity     string `json:"accountEquity"`
		UsdtEquity        string `json:"usdtEquity"`
		UnrealisedPnl     string `json:"unrealisedPnl"`
		UsdtUnrealisedPnl string `json:"usdtUnrealisedPnl"`
		EffEquity         string `json:"effEquity"`
		Assets            []struct {
			Coin      string `json:"coin"`
			Equity    string `json:"equity"`
			Available string `json:"available"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &acc); err != nil {
		return nil, fmt.Errorf("failed to parse UTA balance data: %w, raw: %s", err, string(data))
	}

	totalEquity, _ := types.ParseFloatField("usdtEquity", firstNonEmpty(acc.UsdtEquity, acc.AccountEquity))
	unrealizedPnL, _ := types.ParseFloatField("usdtUnrealisedPnl", firstNonEmpty(acc.UsdtUnrealisedPnl, acc.UnrealisedPnl))
	availableBalance, _ := types.ParseFloatField("effEquity", acc.EffEquity)
	for _, asset := range acc.Assets {
		if strings.EqualFold(asset.Coin, "USDT") {
			if v, err := types.ParseFloatField("available", asset.Available); err == nil {
				availableBalance = v
			}
			break
		}
	}

	logger.Infof("✓ [Bitget UTA] Balance: equity=%.2f, available=%.2f", totalEquity, availableBalance)

	result := map[string]interface{}{
		"totalWalletBalance":    totalEquity - unrealizedPnL,
		"availableBalance":      availableBalance,
		"totalUnrealizedProfit": unrealizedPnL,
		"totalEquity":           totalEquity,
		"total_equity":          totalEquity,
	}

	t.balanceCacheMutex.Lock()
	t.cachedBalance = result
	t.balanceCacheTime = time.Now()
	t.balanceCacheMutex.Unlock()
	return result, nil
}

func (t *BitgetTrader) utaBalanceFromPositions() (map[string]interface{}, error) {
	positions, err := t.utaGetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get account balance: UTA manage permission missing and positions fallback failed: %w", err)
	}
	var margin, unrealized float64
	for _, pos := range positions {
		if v, ok := pos["margin"].(float64); ok {
			margin += v
		}
		if v, ok := pos["unRealizedProfit"].(float64); ok {
			unrealized += v
		}
	}
	equity := margin + unrealized
	logger.Infof("⚠️ [Bitget UTA] /account/assets needs UTA manage read; using position-derived equity=%.2f (enable UTA manage on the API key for free USDT)", equity)
	result := map[string]interface{}{
		"totalWalletBalance":       margin,
		"availableBalance":         0.0,
		"totalUnrealizedProfit":    unrealized,
		"totalEquity":              equity,
		"total_equity":             equity,
		"balancePermissionMissing": true,
	}
	t.balanceCacheMutex.Lock()
	t.cachedBalance = result
	t.balanceCacheTime = time.Now()
	t.balanceCacheMutex.Unlock()
	return result, nil
}

func (t *BitgetTrader) utaGetPositions() ([]map[string]interface{}, error) {
	data, err := t.doRequest("GET", utaCurrentPositionPath, map[string]interface{}{
		"category": utaFuturesCategory,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	var positions []struct {
		Symbol           string `json:"symbol"`
		PosSide          string `json:"posSide"`
		AvgPrice         string `json:"avgPrice"`
		MarkPrice        string `json:"markPrice"`
		Total            string `json:"total"`
		UnrealisedPnl    string `json:"unrealisedPnl"`
		PositionBalance  string `json:"positionBalance"`
		Leverage         string `json:"leverage"`
		LiquidationPrice string `json:"liquidationPrice"`
		MarginMode       string `json:"marginMode"`
		CreatedTime      string `json:"createdTime"`
		UpdatedTime      string `json:"updatedTime"`
	}
	if err := unmarshalUTAList(data, &positions); err != nil {
		return nil, fmt.Errorf("failed to parse UTA position data: %w", err)
	}

	var result []map[string]interface{}
	for _, pos := range positions {
		total, _ := strconv.ParseFloat(pos.Total, 64)
		if total == 0 {
			continue
		}
		entryPrice, _ := strconv.ParseFloat(pos.AvgPrice, 64)
		markPrice, _ := strconv.ParseFloat(pos.MarkPrice, 64)
		unrealizedPnL, _ := strconv.ParseFloat(pos.UnrealisedPnl, 64)
		leverage, _ := strconv.ParseFloat(pos.Leverage, 64)
		liqPrice, _ := strconv.ParseFloat(pos.LiquidationPrice, 64)
		cTime, _ := strconv.ParseInt(pos.CreatedTime, 10, 64)
		uTime, _ := strconv.ParseInt(pos.UpdatedTime, 10, 64)

		side := "long"
		if strings.EqualFold(pos.PosSide, "short") {
			side = "short"
		}

		margin, _ := strconv.ParseFloat(pos.PositionBalance, 64)
		t.rememberUTAMarginMode(pos.Symbol, strings.ToLower(pos.MarginMode))
		result = append(result, map[string]interface{}{
			"symbol":           pos.Symbol,
			"positionAmt":      total,
			"entryPrice":       entryPrice,
			"markPrice":        markPrice,
			"unRealizedProfit": unrealizedPnL,
			"leverage":         leverage,
			"liquidationPrice": liqPrice,
			"margin":           margin,
			"marginMode":       t.utaMarginMode(pos.Symbol),
			"side":             side,
			"createdTime":      cTime,
			"updatedTime":      uTime,
		})
	}

	t.positionsCacheMutex.Lock()
	t.cachedPositions = result
	t.positionsCacheTime = time.Now()
	t.positionsCacheMutex.Unlock()
	return result, nil
}

func (t *BitgetTrader) utaGetClosedPnL(startTime time.Time, limit int) ([]types.ClosedPnLRecord, error) {
	params := map[string]interface{}{
		"category":  utaFuturesCategory,
		"startTime": fmt.Sprintf("%d", startTime.UnixMilli()),
		"limit":     fmt.Sprintf("%d", limit),
	}
	data, err := t.doRequest("GET", utaHistoryPositionPath, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get positions history: %w", err)
	}

	var list []struct {
		Symbol         string `json:"symbol"`
		PosSide        string `json:"posSide"`
		OpenPriceAvg   string `json:"openPriceAvg"`
		ClosePriceAvg  string `json:"closePriceAvg"`
		CloseTotalPos  string `json:"closeTotalPos"`
		CumRealisedPnl string `json:"cumRealisedPnl"`
		NetProfit      string `json:"netProfit"`
		OpenFeeTotal   string `json:"openFeeTotal"`
		CloseFeeTotal  string `json:"closeFeeTotal"`
		CreatedTime    string `json:"createdTime"`
		UpdatedTime    string `json:"updatedTime"`
	}
	if err := unmarshalUTAList(data, &list); err != nil {
		return nil, fmt.Errorf("failed to parse UTA position history: %w", err)
	}

	records := make([]types.ClosedPnLRecord, 0, len(list))
	for _, pos := range list {
		record := types.ClosedPnLRecord{
			Symbol: pos.Symbol,
			Side:   pos.PosSide,
		}
		record.EntryPrice, _ = strconv.ParseFloat(pos.OpenPriceAvg, 64)
		record.ExitPrice, _ = strconv.ParseFloat(pos.ClosePriceAvg, 64)
		record.Quantity, _ = strconv.ParseFloat(pos.CloseTotalPos, 64)
		record.RealizedPnL, _ = strconv.ParseFloat(firstNonEmpty(pos.NetProfit, pos.CumRealisedPnl), 64)
		openFee, _ := strconv.ParseFloat(pos.OpenFeeTotal, 64)
		closeFee, _ := strconv.ParseFloat(pos.CloseFeeTotal, 64)
		record.Fee = openFee + closeFee
		cTime, _ := strconv.ParseInt(pos.CreatedTime, 10, 64)
		uTime, _ := strconv.ParseInt(pos.UpdatedTime, 10, 64)
		record.EntryTime = time.UnixMilli(cTime).UTC()
		record.ExitTime = time.UnixMilli(uTime).UTC()
		record.CloseType = "unknown"
		records = append(records, record)
	}
	return records, nil
}

func (t *BitgetTrader) utaSetLeverage(symbol string, leverage int) error {
	marginMode := t.utaMarginMode(symbol)
	_, err := t.doRequest("POST", utaSetLeveragePath, map[string]interface{}{
		"category":   utaFuturesCategory,
		"symbol":     symbol,
		"leverage":   fmt.Sprintf("%d", leverage),
		"marginMode": marginMode,
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "same") {
			return nil
		}
		logger.Infof("  ⚠️ Failed to set %s leverage: %v", symbol, err)
		return err
	}
	logger.Infof("  ✓ %s leverage set to %dx (%s, UTA)", symbol, leverage, marginMode)
	return nil
}

func (t *BitgetTrader) utaSetMarginMode(symbol string, isCrossMargin bool) error {
	marginMode := "isolated"
	if isCrossMargin {
		marginMode = "crossed"
	}
	// UTA sets marginMode together with leverage and repeats it on each order.
	// Remember the requested mode here; OpenLong/OpenShort call SetLeverage
	// immediately afterward, which performs the authenticated mode update.
	t.rememberUTAMarginMode(symbol, marginMode)
	logger.Infof("  ✓ %s margin mode selected: %s (UTA)", symbol, marginMode)
	return nil
}

func (t *BitgetTrader) utaPlaceMarket(symbol, side, qtyStr, reduceOnly string) (map[string]interface{}, error) {
	marginMode := t.utaMarginMode(symbol)
	body := map[string]interface{}{
		"category":   utaFuturesCategory,
		"symbol":     symbol,
		"qty":        qtyStr,
		"side":       side,
		"orderType":  "market",
		"marginMode": marginMode,
		"reduceOnly": reduceOnly,
		"clientOid":  genBitgetClientOid(),
	}
	data, err := t.doRequest("POST", utaPlaceOrderPath, body)
	if err != nil {
		return nil, err
	}
	var order struct {
		OrderId   string `json:"orderId"`
		ClientOid string `json:"clientOid"`
	}
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, fmt.Errorf("failed to parse UTA order response: %w", err)
	}
	t.clearCache()
	return map[string]interface{}{
		"orderId": order.OrderId,
		"symbol":  symbol,
		"status":  "FILLED",
	}, nil
}

func (t *BitgetTrader) utaPlaceLimit(symbol, side, qtyStr, price string, reduceOnly bool) (string, string, error) {
	marginMode := t.utaMarginMode(symbol)
	body := map[string]interface{}{
		"category":    utaFuturesCategory,
		"symbol":      symbol,
		"qty":         qtyStr,
		"side":        side,
		"orderType":   "limit",
		"price":       price,
		"timeInForce": "gtc",
		"marginMode":  marginMode,
		"reduceOnly":  "no",
		"clientOid":   genBitgetClientOid(),
	}
	if reduceOnly {
		body["reduceOnly"] = "yes"
	}
	data, err := t.doRequest("POST", utaPlaceOrderPath, body)
	if err != nil {
		return "", "", err
	}
	var order struct {
		OrderId   string `json:"orderId"`
		ClientOid string `json:"clientOid"`
	}
	if err := json.Unmarshal(data, &order); err != nil {
		return "", "", fmt.Errorf("failed to parse UTA order response: %w", err)
	}
	return order.OrderId, order.ClientOid, nil
}

func (t *BitgetTrader) utaCancelOrder(orderID string) error {
	_, err := t.doRequest("POST", utaCancelOrderPath, map[string]interface{}{
		"orderId":  orderID,
		"category": utaFuturesCategory,
	})
	return err
}

func (t *BitgetTrader) utaCancelAllOrders(symbol string) error {
	_, err := t.doRequest("POST", utaCancelSymbolOrderPath, map[string]interface{}{
		"category": utaFuturesCategory,
		"symbol":   symbol,
	})
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not exist") {
		return err
	}
	_ = t.utaCancelPlanOrders(symbol, "loss")
	_ = t.utaCancelPlanOrders(symbol, "profit")
	return nil
}

func (t *BitgetTrader) utaSetStopLoss(symbol, positionSide string, quantity, stopPrice float64) error {
	return t.utaPlaceCombinedTPSL(symbol, positionSide, quantity, stopPrice, 0)
}

func (t *BitgetTrader) utaSetTakeProfit(symbol, positionSide string, quantity, takeProfitPrice float64) error {
	return t.utaPlaceCombinedTPSL(symbol, positionSide, quantity, 0, takeProfitPrice)
}

// SetTPSL places stop-loss and take-profit in one UTA strategy order.
// Bitget UTA rejects SL-only payloads with "Parameter takeProfit cannot be empty".
func (t *BitgetTrader) SetTPSL(symbol, positionSide string, quantity, stopLoss, takeProfit float64) error {
	symbol = t.convertSymbol(symbol)
	if t.useUTA() {
		return t.utaPlaceCombinedTPSL(symbol, positionSide, quantity, stopLoss, takeProfit)
	}
	if err := t.SetStopLoss(symbol, positionSide, quantity, stopLoss); err != nil {
		return err
	}
	if takeProfit > 0 {
		return t.SetTakeProfit(symbol, positionSide, quantity, takeProfit)
	}
	return nil
}

func (t *BitgetTrader) utaPlaceTPSL(symbol, positionSide string, quantity, trigger float64, isStopLoss bool) error {
	if isStopLoss {
		return t.utaPlaceCombinedTPSL(symbol, positionSide, quantity, trigger, 0)
	}
	return t.utaPlaceCombinedTPSL(symbol, positionSide, quantity, 0, trigger)
}

func (t *BitgetTrader) utaPlaceCombinedTPSL(symbol, positionSide string, quantity, stopLoss, takeProfit float64) error {
	if stopLoss <= 0 && takeProfit <= 0 {
		return fmt.Errorf("Bitget UTA TPSL requires a stop or take-profit price")
	}
	// UTA place-strategy-order (type=tpsl) requires BOTH fields. If only one
	// side was asked for, synthesize a wide counterpart so Bitget accepts it.
	if stopLoss <= 0 {
		if strings.EqualFold(positionSide, "SHORT") {
			stopLoss = takeProfit * 1.20
		} else {
			stopLoss = takeProfit * 0.80
		}
	}
	if takeProfit <= 0 {
		if strings.EqualFold(positionSide, "SHORT") {
			takeProfit = stopLoss * 0.80
		} else {
			takeProfit = stopLoss * 1.20
		}
	}
	slStr := t.FormatPrice(symbol, stopLoss)
	tpStr := t.FormatPrice(symbol, takeProfit)
	qtyStr := ""
	if quantity > 0 {
		qtyStr, _ = t.FormatQuantity(symbol, quantity)
	}
	body := utaTPSLBody(symbol, positionSide, slStr, tpStr, qtyStr, genBitgetClientOid())
	if _, err := t.doRequest("POST", utaPlaceStrategyPath, body); err != nil {
		return fmt.Errorf("failed to set stop loss: %w", err)
	}
	logger.Infof("  ✓ [Bitget UTA] SL/TP set: %s sl=%s tp=%s", symbol, slStr, tpStr)
	return nil
}

func utaTPSLBody(symbol, positionSide, stopLoss, takeProfit, qty, clientOid string) map[string]interface{} {
	body := map[string]interface{}{
		"category":    utaFuturesCategory,
		"symbol":      symbol,
		"type":        "tpsl",
		"tpslMode":    "full",
		"tpTriggerBy": "mark",
		"slTriggerBy": "mark",
		"tpOrderType": "market",
		"slOrderType": "market",
		"clientOid":   clientOid,
		"stopLoss":    stopLoss,
		"takeProfit":  takeProfit,
	}
	if strings.EqualFold(positionSide, "SHORT") {
		body["posSide"] = "short"
	} else if strings.EqualFold(positionSide, "LONG") {
		body["posSide"] = "long"
	}
	if qty != "" {
		body["tpslMode"] = "partial"
		body["qty"] = qty
	}
	return body
}

func (t *BitgetTrader) utaCancelPlanOrders(symbol, want string) error {
	data, err := t.doRequest("GET", utaUnfilledStrategyPath, map[string]interface{}{
		"category": utaFuturesCategory,
		"type":     "tpsl",
	})
	if err != nil {
		return err
	}
	orders, err := parseUTAStrategyOrders(data)
	if err != nil {
		return err
	}
	for _, order := range orders {
		if symbol != "" && !strings.EqualFold(order.Symbol, symbol) {
			continue
		}
		hasSL := strings.TrimSpace(order.StopLoss) != ""
		hasTP := strings.TrimSpace(order.TakeProfit) != ""
		match := (want == "loss" && hasSL) || (want == "profit" && hasTP) || want == "all"
		if !match {
			continue
		}
		_, _ = t.doRequest("POST", utaCancelStrategyPath, map[string]interface{}{
			"orderId": order.OrderId,
		})
	}
	return nil
}

func (t *BitgetTrader) utaGetOrderStatus(symbol, orderID string) (map[string]interface{}, error) {
	data, err := t.doRequest("GET", utaOrderInfoPath, map[string]interface{}{
		"orderId": orderID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get order status: %w", err)
	}
	var order struct {
		OrderId     string `json:"orderId"`
		OrderStatus string `json:"orderStatus"`
		AvgPrice    string `json:"avgPrice"`
		CumExecQty  string `json:"cumExecQty"`
		Side        string `json:"side"`
		OrderType   string `json:"orderType"`
		CreatedTime string `json:"createdTime"`
		UpdatedTime string `json:"updatedTime"`
		FeeDetail   []struct {
			Fee string `json:"fee"`
		} `json:"feeDetail"`
	}
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, err
	}
	avgPrice, _ := strconv.ParseFloat(order.AvgPrice, 64)
	fillQty, _ := strconv.ParseFloat(order.CumExecQty, 64)
	var fee float64
	if len(order.FeeDetail) > 0 {
		fee, _ = strconv.ParseFloat(order.FeeDetail[0].Fee, 64)
	}
	cTime, _ := strconv.ParseInt(order.CreatedTime, 10, 64)
	uTime, _ := strconv.ParseInt(order.UpdatedTime, 10, 64)
	statusMap := map[string]string{
		"filled":           "FILLED",
		"live":             "NEW",
		"new":              "NEW",
		"partially_filled": "PARTIALLY_FILLED",
		"cancelled":        "CANCELED",
		"canceled":         "CANCELED",
	}
	status := statusMap[strings.ToLower(order.OrderStatus)]
	if status == "" {
		status = order.OrderStatus
	}
	return map[string]interface{}{
		"orderId":     order.OrderId,
		"symbol":      symbol,
		"status":      status,
		"avgPrice":    avgPrice,
		"executedQty": fillQty,
		"side":        order.Side,
		"type":        order.OrderType,
		"time":        cTime,
		"updateTime":  uTime,
		"commission":  -math.Abs(fee),
	}, nil
}

func (t *BitgetTrader) utaGetOpenOrders(symbol string) ([]types.OpenOrder, error) {
	var result []types.OpenOrder
	data, err := t.doRequest("GET", utaUnfilledOrdersPath, map[string]interface{}{
		"category": utaFuturesCategory,
		"symbol":   symbol,
	})
	if err != nil {
		logger.Warnf("[Bitget UTA] Failed to get pending orders: %v", err)
	} else {
		var orders []struct {
			OrderId   string `json:"orderId"`
			Symbol    string `json:"symbol"`
			Side      string `json:"side"`
			PosSide   string `json:"posSide"`
			OrderType string `json:"orderType"`
			Price     string `json:"price"`
			Qty       string `json:"qty"`
		}
		if err := unmarshalUTAList(data, &orders); err == nil {
			for _, order := range orders {
				price, _ := strconv.ParseFloat(order.Price, 64)
				quantity, _ := strconv.ParseFloat(order.Qty, 64)
				result = append(result, types.OpenOrder{
					OrderID:      order.OrderId,
					Symbol:       firstNonEmpty(order.Symbol, symbol),
					Side:         strings.ToUpper(order.Side),
					PositionSide: strings.ToUpper(order.PosSide),
					Type:         strings.ToUpper(order.OrderType),
					Price:        price,
					Quantity:     quantity,
					Status:       "NEW",
				})
			}
		}
	}

	planData, err := t.doRequest("GET", utaUnfilledStrategyPath, map[string]interface{}{
		"category": utaFuturesCategory,
		"type":     "tpsl",
	})
	if err != nil {
		logger.Warnf("[Bitget UTA] Failed to get strategy orders: %v", err)
	} else {
		orders, parseErr := parseUTAStrategyOrders(planData)
		if parseErr == nil {
			for _, order := range orders {
				if symbol != "" && !strings.EqualFold(order.Symbol, symbol) {
					continue
				}
				qty, _ := strconv.ParseFloat(order.Qty, 64)
				if tp, _ := strconv.ParseFloat(order.TakeProfit, 64); tp > 0 {
					result = append(result, types.OpenOrder{
						OrderID:      order.OrderId,
						Symbol:       order.Symbol,
						PositionSide: strings.ToUpper(order.PosSide),
						Type:         "TAKE_PROFIT_MARKET",
						StopPrice:    tp,
						Quantity:     qty,
						Status:       "NEW",
					})
				}
				if sl, _ := strconv.ParseFloat(order.StopLoss, 64); sl > 0 {
					result = append(result, types.OpenOrder{
						OrderID:      order.OrderId,
						Symbol:       order.Symbol,
						PositionSide: strings.ToUpper(order.PosSide),
						Type:         "STOP_MARKET",
						StopPrice:    sl,
						Quantity:     qty,
						Status:       "NEW",
					})
				}
			}
		}
	}

	logger.Infof("✓ BITGET UTA GetOpenOrders: found %d open orders for %s", len(result), symbol)
	return result, nil
}

func (t *BitgetTrader) utaGetTrades(startTime time.Time, limit int) ([]BitgetTrade, error) {
	data, err := t.doRequest("GET", utaFillsPath, map[string]interface{}{
		"category":  utaFuturesCategory,
		"startTime": fmt.Sprintf("%d", startTime.UnixMilli()),
		"limit":     fmt.Sprintf("%d", limit),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get fill history: %w", err)
	}
	fills, err := parseUTAFills(data)
	if err != nil {
		logger.Infof("⚠️ Bitget UTA fills parse failed, raw: %s", string(data))
		return nil, fmt.Errorf("failed to parse fills: %w", err)
	}
	logger.Infof("🔍 Bitget UTA: parsed fills count: %d", len(fills))

	trades := make([]BitgetTrade, 0, len(fills))
	for _, fill := range fills {
		fillPrice, _ := strconv.ParseFloat(fill.ExecPrice, 64)
		fillQty, _ := strconv.ParseFloat(fill.ExecQty, 64)
		profit, _ := strconv.ParseFloat(fill.ExecPnl, 64)
		orderAction, ok := utaOrderAction(fill.Side, fill.TradeSide, fill.HoldSide, fill.PosSide, profit)
		if !ok {
			logger.Warnf("⚠️ Bitget UTA: skipping ambiguous fill %s (side=%q tradeSide=%q holdSide=%q posSide=%q)",
				firstNonEmpty(fill.ExecId, fill.OrderId), fill.Side, fill.TradeSide, fill.HoldSide, fill.PosSide)
			continue
		}
		cTime, _ := strconv.ParseInt(fill.CreatedTime, 10, 64)
		var fee float64
		var feeAsset string
		if len(fill.FeeDetail) > 0 {
			fee, _ = strconv.ParseFloat(fill.FeeDetail[0].Fee, 64)
			feeAsset = fill.FeeDetail[0].FeeCoin
		}
		trades = append(trades, BitgetTrade{
			Symbol:      fill.Symbol,
			TradeID:     firstNonEmpty(fill.ExecId, fill.OrderId),
			OrderID:     fill.OrderId,
			Side:        fill.Side,
			FillPrice:   fillPrice,
			FillQty:     fillQty,
			Fee:         -math.Abs(fee),
			FeeAsset:    feeAsset,
			ExecTime:    time.UnixMilli(cTime).UTC(),
			ProfitLoss:  profit,
			OrderType:   strings.ToUpper(fill.OrderType),
			OrderAction: orderAction,
		})
	}
	return trades, nil
}

type utaFill struct {
	ExecId      string `json:"execId"`
	OrderId     string `json:"orderId"`
	Symbol      string `json:"symbol"`
	OrderType   string `json:"orderType"`
	Side        string `json:"side"`
	TradeSide   string `json:"tradeSide"`
	HoldSide    string `json:"holdSide"`
	PosSide     string `json:"posSide"`
	ExecPrice   string `json:"execPrice"`
	ExecQty     string `json:"execQty"`
	ExecPnl     string `json:"execPnl"`
	CreatedTime string `json:"createdTime"`
	FeeDetail   []struct {
		FeeCoin string `json:"feeCoin"`
		Fee     string `json:"fee"`
	} `json:"feeDetail"`
}

type utaStrategyOrder struct {
	OrderId    string `json:"orderId"`
	Symbol     string `json:"symbol"`
	Qty        string `json:"qty"`
	PosSide    string `json:"posSide"`
	TakeProfit string `json:"takeProfit"`
	StopLoss   string `json:"stopLoss"`
}

func parseUTAFills(data []byte) ([]utaFill, error) {
	var fills []utaFill
	if err := unmarshalUTAList(data, &fills); err != nil {
		return nil, err
	}
	return fills, nil
}

func parseUTAStrategyOrders(data []byte) ([]utaStrategyOrder, error) {
	var orders []utaStrategyOrder
	if err := unmarshalUTAList(data, &orders); err != nil {
		return nil, err
	}
	return orders, nil
}

func unmarshalUTAList(data []byte, dest interface{}) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err == nil {
		if raw, ok := obj["list"]; ok {
			if len(raw) == 0 || string(raw) == "null" {
				return json.Unmarshal([]byte("[]"), dest)
			}
			return json.Unmarshal(raw, dest)
		}
	}
	return json.Unmarshal(data, dest)
}

func utaOrderAction(side, tradeSide, holdSide, posSide string, execPnl float64) (string, bool) {
	side = strings.ToLower(side)
	tradeSide = strings.ToLower(tradeSide)
	positionSide := strings.ToLower(firstNonEmpty(holdSide, posSide))
	switch tradeSide {
	case "open":
		if side == "buy" {
			return "open_long", true
		}
		if side == "sell" {
			return "open_short", true
		}
	case "close":
		if side == "buy" {
			return "close_short", true
		}
		if side == "sell" {
			return "close_long", true
		}
	case "buy_single", "sell_single", "":
		// Hedge fills include holdSide/posSide. One-way UTA fills often omit both;
		// disambiguate open vs close with execPnl (realized on closes).
		switch {
		case side == "buy" && positionSide == "long":
			return "open_long", true
		case side == "buy" && positionSide == "short":
			return "close_short", true
		case side == "sell" && positionSide == "short":
			return "open_short", true
		case side == "sell" && positionSide == "long":
			return "close_long", true
		}
		return utaOneWayOrderAction(side, tradeSide, execPnl)
	}
	return "", false
}

// utaOneWayOrderAction maps Bitget one-way fills when holdSide/posSide are absent.
// Opens carry zero execPnl; closes carry realized PnL from the exchange.
func utaOneWayOrderAction(side, tradeSide string, execPnl float64) (string, bool) {
	side = strings.ToLower(side)
	tradeSide = strings.ToLower(tradeSide)
	if side == "" {
		return "", false
	}
	if execPnl != 0 {
		if side == "buy" {
			return "close_short", true
		}
		if side == "sell" {
			return "close_long", true
		}
	}
	if side == "buy" || tradeSide == "buy_single" {
		return "open_long", true
	}
	if side == "sell" || tradeSide == "sell_single" {
		return "open_short", true
	}
	return "", false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
