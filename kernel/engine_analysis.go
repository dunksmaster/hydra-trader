package kernel

import (
	"encoding/json"
	"fmt"
	"nofx/logger"
	"nofx/market"
	"nofx/mcp"
	"nofx/store"
	"regexp"
	"strings"
	"time"
)

// ============================================================================
// Pre-compiled regular expressions (performance optimization)
// ============================================================================

var (
	// Safe regex: precisely match ```json code blocks
	reJSONFence      = regexp.MustCompile(`(?is)` + "```json\\s*(\\[\\s*\\{.*?\\}\\s*\\])\\s*```")
	reJSONArray      = regexp.MustCompile(`(?is)\[\s*\{.*?\}\s*\]`)
	reArrayHead      = regexp.MustCompile(`^\[\s*\{`)
	reArrayOpenSpace = regexp.MustCompile(`^\[\s+\{`)
	reInvisibleRunes = regexp.MustCompile("[\u200B\u200C\u200D\uFEFF]")

	// XML tag extraction (supports any characters in reasoning chain)
	reReasoningTag = regexp.MustCompile(`(?s)<reasoning>(.*?)</reasoning>`)
	reDecisionTag  = regexp.MustCompile(`(?s)<decision>(.*?)</decision>`)

	// Nemotron often drafts JSON with an ellipsis placeholder: {"action":"wait", ...}
	reJSONEllipsisComma = regexp.MustCompile(`,\s*(?:\.{3}|…)`)
	reJSONEllipsisBare  = regexp.MustCompile(`(?:\.{3}|…)\s*`)
	reJSONTrailingComma = regexp.MustCompile(`,\s*([}\]])`)
)

// ============================================================================
// Entry Functions - Main API
// ============================================================================

// GetFullDecision gets AI's complete trading decision (batch analysis of all coins and positions)
// Uses default strategy configuration - for production use GetFullDecisionWithStrategy with explicit config
func GetFullDecision(ctx *Context, mcpClient mcp.AIClient) (*FullDecision, error) {
	defaultConfig := store.GetDefaultStrategyConfig("en")
	engine := NewStrategyEngine(&defaultConfig)
	return GetFullDecisionWithStrategy(ctx, mcpClient, engine, "")
}

// GetFullDecisionWithStrategy uses StrategyEngine to get AI decision (unified prompt generation)
func GetFullDecisionWithStrategy(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine, variant string) (*FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if engine == nil {
		defaultConfig := store.GetDefaultStrategyConfig("en")
		engine = NewStrategyEngine(&defaultConfig)
	}

	// Clamp strategy limits to prevent token overflow
	engineConfig := engine.GetConfig()
	engineConfig.ClampLimits()

	// Token estimation check — block if exceeding the specific model's context limit
	estimate := engineConfig.EstimateTokens()

	// Determine context limit for the specific model being used
	contextLimit := 131072 // safe default (strictest common limit)
	var providerName string
	if embedder, ok := mcpClient.(mcp.ClientEmbedder); ok {
		base := embedder.BaseClient()
		providerName = base.Provider
		contextLimit = store.GetContextLimitForClient(base.Provider, base.Model)
	}

	if estimate.Total > contextLimit {
		logger.Errorf("🚫 Token estimate %d exceeds %s context limit %d — blocking analysis",
			estimate.Total, providerName, contextLimit)
		return nil, fmt.Errorf("estimated %d tokens exceeds model context limit of %d; reduce coins, timeframes, or K-line count",
			estimate.Total, contextLimit)
	}
	if estimate.Total*100/contextLimit >= 80 {
		logger.Infof("⚠️  Token estimate %d — approaching %s context limit %d",
			estimate.Total, providerName, contextLimit)
	}

	// 1. Fetch market data using strategy config
	if len(ctx.MarketDataMap) == 0 {
		if err := fetchMarketDataWithStrategy(ctx, engine); err != nil {
			return nil, fmt.Errorf("failed to fetch market data: %w", err)
		}
	}
	pruneCandidateCoinsWithoutMarketData(ctx)
	enrichVergexDataWithStrategy(ctx, engine)

	// Optional OI-top enrichment. This used to run on every cycle and would
	// block Bitget/hyper_rank traders on a paid NofxOS/claw402 call (5m timeout)
	// before NVIDIA ever ran.
	if ctx.OITopDataMap == nil {
		ctx.OITopDataMap = make(map[string]*OITopData)
		if shouldFetchLegacyOITop(engine) && engine.nofxosClient != nil {
			logger.Infof("📊 Fetching legacy NofxOS OI-top enrichment")
			oiPositions, err := engine.nofxosClient.GetOITopPositions()
			if err == nil {
				for _, pos := range oiPositions {
					ctx.OITopDataMap[pos.Symbol] = &OITopData{
						Rank:              pos.Rank,
						OIDeltaPercent:    pos.OIDeltaPercent,
						OIDeltaValue:      pos.OIDeltaValue,
						PriceDeltaPercent: pos.PriceDeltaPercent,
					}
				}
			} else {
				logger.Warnf("⚠️  Failed to fetch NofxOS OI-top enrichment: %v", err)
			}
		} else {
			logger.Infof("⏭️  Skipping legacy NofxOS OI-top enrichment (hyper_rank or OI ranking off)")
		}
	}

	// 2. Build System Prompt using strategy engine
	riskConfig := engine.GetRiskControlConfig()
	systemPrompt := engine.BuildSystemPrompt(ctx.Account.TotalEquity, variant)

	// 3. Build User Prompt using strategy engine
	userPrompt := engine.BuildUserPrompt(ctx)

	// 4. Call AI API
	aiCallStart := time.Now()
	aiResponse, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	aiCallDuration := time.Since(aiCallStart)
	if err != nil {
		return nil, fmt.Errorf("AI API call failed: %w", err)
	}

	// 5. Parse AI response
	decision, err := parseFullDecisionResponse(
		aiResponse,
		ctx.Account.TotalEquity,
		riskConfig.BTCETHMaxLeverage,
		riskConfig.AltcoinMaxLeverage,
		riskConfig.BTCETHMaxPositionValueRatio,
		riskConfig.AltcoinMaxPositionValueRatio,
	)

	if decision != nil {
		decision.Timestamp = time.Now()
		decision.SystemPrompt = systemPrompt
		decision.UserPrompt = userPrompt
		decision.AIRequestDurationMs = aiCallDuration.Milliseconds()
		decision.RawResponse = aiResponse
	}

	if err != nil {
		return decision, fmt.Errorf("failed to parse AI response: %w", err)
	}

	return decision, nil
}

func enrichVergexDataWithStrategy(ctx *Context, engine *StrategyEngine) {
	if ctx == nil || engine == nil || ctx.VergexDataMap != nil {
		return
	}
	if engine.GetConfig().CoinSource.SourceType != "vergex_signal" {
		return
	}
	symbolSet := make(map[string]bool)
	symbols := make([]string, 0, len(ctx.CandidateCoins)+len(ctx.Positions))
	for _, coin := range ctx.CandidateCoins {
		if !symbolSet[coin.Symbol] {
			symbolSet[coin.Symbol] = true
			symbols = append(symbols, coin.Symbol)
		}
	}
	for _, pos := range ctx.Positions {
		if !symbolSet[pos.Symbol] {
			symbolSet[pos.Symbol] = true
			symbols = append(symbols, pos.Symbol)
		}
	}
	ctx.VergexDataMap = engine.FetchVergexDataBatch(engine.chargeContext(), symbols)
}

// ============================================================================
// Market Data Fetching
// ============================================================================

// fetchMarketDataWithStrategy fetches market data using strategy config (multiple timeframes)
func fetchMarketDataWithStrategy(ctx *Context, engine *StrategyEngine) error {
	config := engine.GetConfig()
	ctx.MarketDataMap = make(map[string]*market.Data)

	timeframes := config.Indicators.Klines.SelectedTimeframes
	primaryTimeframe := config.Indicators.Klines.PrimaryTimeframe
	klineCount := config.Indicators.Klines.PrimaryCount

	// Compatible with old configuration
	if len(timeframes) == 0 {
		if primaryTimeframe != "" {
			timeframes = append(timeframes, primaryTimeframe)
		} else {
			timeframes = append(timeframes, "3m")
		}
		if config.Indicators.Klines.LongerTimeframe != "" {
			timeframes = append(timeframes, config.Indicators.Klines.LongerTimeframe)
		}
	}
	if primaryTimeframe == "" {
		primaryTimeframe = timeframes[0]
	}
	if klineCount <= 0 {
		klineCount = 30
	}

	logger.Infof("📊 Strategy timeframes: %v, Primary: %s, Kline count: %d", timeframes, primaryTimeframe, klineCount)

	klineExchange := market.NormalizeKlineExchange("")
	if ctx.MarketDataExchange != "" {
		klineExchange = market.NormalizeKlineExchange(ctx.MarketDataExchange)
	}
	strictKlines := strings.EqualFold(klineExchange, "bitget")
	if strictKlines {
		logger.Infof("📊 Using %s klines for AI market data (match execution venue)", klineExchange)
	}

	// 1. First fetch data for position coins (must fetch)
	for _, pos := range ctx.Positions {
		data, err := market.GetWithTimeframesForExchange(pos.Symbol, timeframes, primaryTimeframe, klineCount, klineExchange, strictKlines)
		if err != nil {
			logger.Infof("⚠️  Failed to fetch market data for position %s: %v", pos.Symbol, err)
			continue
		}
		ctx.MarketDataMap[pos.Symbol] = data
	}

	// 2. Fetch data for all candidate coins
	positionSymbols := make(map[string]bool)
	for _, pos := range ctx.Positions {
		positionSymbols[pos.Symbol] = true
	}

	const minOIThresholdMillions = 15.0 // 15M USD minimum open interest value

	for _, coin := range ctx.CandidateCoins {
		if _, exists := ctx.MarketDataMap[coin.Symbol]; exists {
			continue
		}

		data, err := market.GetWithTimeframesForExchange(coin.Symbol, timeframes, primaryTimeframe, klineCount, klineExchange, strictKlines)
		if err != nil {
			logger.Infof("⚠️  Failed to fetch market data for %s: %v", coin.Symbol, err)
			continue
		}

		// Liquidity filter (skip for xyz dex assets - they don't have OI data from Binance)
		isExistingPosition := positionSymbols[coin.Symbol]
		isXyzAsset := market.IsXyzDexAsset(coin.Symbol)
		if !isExistingPosition && !isXyzAsset && shouldSkipLowOpenInterest(data, minOIThresholdMillions) {
			oiValueInMillions := data.OpenInterest.Latest * data.CurrentPrice / 1_000_000
			logger.Infof("⚠️  %s OI value too low (%.2fM USD < %.1fM), skipping coin",
				coin.Symbol, oiValueInMillions, minOIThresholdMillions)
			continue
		}

		ctx.MarketDataMap[coin.Symbol] = data
	}

	logger.Infof("📊 Successfully fetched multi-timeframe market data for %d coins", len(ctx.MarketDataMap))
	return nil
}

// shouldFetchLegacyOITop is the old always-on NofxOS OI-top prompt enrichment.
// Skip it for Hyperliquid-native boards (hyper_rank / vergex) and whenever the
// strategy did not ask for OI ranking — those calls go through claw402 and can
// stall the whole decision cycle for minutes.
func shouldFetchLegacyOITop(engine *StrategyEngine) bool {
	if engine == nil || engine.config == nil {
		return false
	}
	if engine.usesHyperliquidNativeUniverse() {
		return false
	}
	source := engine.config.CoinSource
	return engine.config.Indicators.EnableOIRanking || source.UseOITop || source.SourceType == "oi_top"
}

// shouldSkipLowOpenInterest applies the liquidity floor only when Binance
// returned a real OI reading. Latest==0 is the fetch-failure fallback and
// must not empty the candidate list.
func shouldSkipLowOpenInterest(data *market.Data, minMillions float64) bool {
	if data == nil || data.OpenInterest == nil || data.OpenInterest.Latest <= 0 || data.CurrentPrice <= 0 {
		return false
	}
	return data.OpenInterest.Latest*data.CurrentPrice/1_000_000 < minMillions
}

func pruneCandidateCoinsWithoutMarketData(ctx *Context) {
	if ctx == nil || len(ctx.CandidateCoins) == 0 || len(ctx.MarketDataMap) == 0 {
		return
	}
	kept := make([]CandidateCoin, 0, len(ctx.CandidateCoins))
	for _, coin := range ctx.CandidateCoins {
		if _, ok := ctx.MarketDataMap[coin.Symbol]; ok {
			kept = append(kept, coin)
			continue
		}
		logger.Infof("⚠️  Skipping candidate %s in AI prompt: no valid market/K-line data", coin.Symbol)
	}
	ctx.CandidateCoins = kept
}

// ============================================================================
// AI Response Parsing
// ============================================================================

func parseFullDecisionResponse(aiResponse string, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio float64) (*FullDecision, error) {
	cotTrace := extractCoTTrace(aiResponse)

	decisions, err := extractDecisions(aiResponse)
	if err != nil {
		return &FullDecision{
			CoTTrace:  cotTrace,
			Decisions: []Decision{},
		}, fmt.Errorf("failed to extract decisions: %w", err)
	}

	if err := validateDecisions(decisions, accountEquity, btcEthLeverage, altcoinLeverage, btcEthPosRatio, altcoinPosRatio); err != nil {
		return &FullDecision{
			CoTTrace:  cotTrace,
			Decisions: decisions,
		}, fmt.Errorf("decision validation failed: %w", err)
	}

	return &FullDecision{
		CoTTrace:  cotTrace,
		Decisions: decisions,
	}, nil
}

func extractCoTTrace(response string) string {
	if match := reReasoningTag.FindStringSubmatch(response); match != nil && len(match) > 1 {
		logger.Infof("✓ Extracted reasoning chain using <reasoning> tag")
		return strings.TrimSpace(match[1])
	}

	if decisionIdx := strings.Index(response, "<decision>"); decisionIdx > 0 {
		logger.Infof("✓ Extracted content before <decision> tag as reasoning chain")
		return strings.TrimSpace(response[:decisionIdx])
	}

	jsonStart := strings.Index(response, "[")
	if jsonStart > 0 {
		logger.Infof("⚠️  Extracted reasoning chain using old format ([ character separator)")
		return strings.TrimSpace(response[:jsonStart])
	}

	return strings.TrimSpace(response)
}

func extractDecisions(response string) ([]Decision, error) {
	s := removeInvisibleRunes(response)
	s = strings.TrimSpace(s)
	s = fixMissingQuotes(s)

	jsonPart := lastDecisionJSONPart(s)
	jsonPart = fixMissingQuotes(jsonPart)
	jsonPart = sanitizeDecisionJSON(jsonPart)

	if isEmptyDecisionArray(jsonPart) {
		logger.Infof("⚠️  [SafeFallback] <decision> was empty []; treating as wait")
		return []Decision{waitDecision("Model output an empty decision list")}, nil
	}

	if m := reJSONFence.FindStringSubmatch(jsonPart); m != nil && len(m) > 1 {
		jsonContent := prepareDecisionJSON(m[1])
		decisions, err := unmarshalDecisions(jsonContent)
		if err != nil {
			logger.Warnf("⚠️  [SafeFallback] fenced JSON parse failed (%v); waiting this cycle", err)
			return []Decision{waitDecision(err.Error())}, nil
		}
		return normalizeWaitDecisions(decisions), nil
	}

	jsonContent := strings.TrimSpace(reJSONArray.FindString(jsonPart))
	if jsonContent == "" {
		logger.Infof("⚠️  [SafeFallback] AI didn't output JSON decision, entering safe wait mode")
		cotSummary := jsonPart
		if len(cotSummary) > 240 {
			cotSummary = cotSummary[:240] + "..."
		}
		return []Decision{waitDecision("Model didn't output structured JSON decision, entering safe wait; summary: " + cotSummary)}, nil
	}

	jsonContent = prepareDecisionJSON(jsonContent)
	decisions, err := unmarshalDecisions(jsonContent)
	if err != nil {
		logger.Warnf("⚠️  [SafeFallback] JSON parse failed (%v); waiting this cycle. content=%s", err, jsonContent)
		return []Decision{waitDecision("JSON parse failed, safe wait: " + err.Error())}, nil
	}
	return normalizeWaitDecisions(decisions), nil
}

func lastDecisionJSONPart(s string) string {
	matches := reDecisionTag.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		logger.Infof("⚠️  <decision> tag not found, searching JSON in full text")
		return s
	}
	logger.Infof("✓ Extracted JSON using last of %d <decision> tag(s)", len(matches))
	return strings.TrimSpace(matches[len(matches)-1][1])
}

func prepareDecisionJSON(s string) string {
	s = compactArrayOpen(strings.TrimSpace(s))
	s = fixMissingQuotes(s)
	return sanitizeDecisionJSON(s)
}

func sanitizeDecisionJSON(s string) string {
	s = reJSONEllipsisComma.ReplaceAllString(s, "")
	s = reJSONEllipsisBare.ReplaceAllString(s, "")
	s = reJSONTrailingComma.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

func isEmptyDecisionArray(s string) bool {
	return strings.TrimSpace(s) == "[]"
}

func waitDecision(reason string) Decision {
	return Decision{Symbol: "ALL", Action: "wait", Reasoning: reason}
}

func unmarshalDecisions(jsonContent string) ([]Decision, error) {
	if isEmptyDecisionArray(jsonContent) {
		return []Decision{waitDecision("Model output an empty decision list")}, nil
	}
	if err := validateJSONFormat(jsonContent); err != nil {
		return nil, err
	}
	var decisions []Decision
	if err := json.Unmarshal([]byte(jsonContent), &decisions); err != nil {
		return nil, err
	}
	return decisions, nil
}

func normalizeWaitDecisions(decisions []Decision) []Decision {
	for i := range decisions {
		if strings.EqualFold(decisions[i].Action, "wait") && strings.TrimSpace(decisions[i].Symbol) == "" {
			decisions[i].Symbol = "ALL"
		}
		if strings.EqualFold(decisions[i].Symbol, "wait") {
			decisions[i].Symbol = "ALL"
			if decisions[i].Action == "" {
				decisions[i].Action = "wait"
			}
		}
	}
	return decisions
}

func fixMissingQuotes(jsonStr string) string {
	jsonStr = strings.ReplaceAll(jsonStr, "\u201c", "\"")
	jsonStr = strings.ReplaceAll(jsonStr, "\u201d", "\"")
	jsonStr = strings.ReplaceAll(jsonStr, "\u2018", "'")
	jsonStr = strings.ReplaceAll(jsonStr, "\u2019", "'")

	jsonStr = strings.ReplaceAll(jsonStr, "［", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "］", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "｛", "{")
	jsonStr = strings.ReplaceAll(jsonStr, "｝", "}")
	jsonStr = strings.ReplaceAll(jsonStr, "：", ":")
	jsonStr = strings.ReplaceAll(jsonStr, "，", ",")

	jsonStr = strings.ReplaceAll(jsonStr, "【", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "】", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "〔", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "〕", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "、", ",")

	jsonStr = strings.ReplaceAll(jsonStr, "　", " ")

	return jsonStr
}

func validateJSONFormat(jsonStr string) error {
	trimmed := strings.TrimSpace(jsonStr)

	if !reArrayHead.MatchString(trimmed) {
		if strings.HasPrefix(trimmed, "[") && !strings.Contains(trimmed[:min(20, len(trimmed))], "{") {
			return fmt.Errorf("not a valid decision array (must contain objects {}), actual content: %s", trimmed[:min(50, len(trimmed))])
		}
		return fmt.Errorf("JSON must start with [{ (whitespace allowed), actual: %s", trimmed[:min(20, len(trimmed))])
	}

	if strings.Contains(jsonStr, "~") {
		return fmt.Errorf("JSON cannot contain range symbol ~, all numbers must be precise single values")
	}

	for i := 0; i < len(jsonStr)-4; i++ {
		if jsonStr[i] >= '0' && jsonStr[i] <= '9' &&
			jsonStr[i+1] == ',' &&
			jsonStr[i+2] >= '0' && jsonStr[i+2] <= '9' &&
			jsonStr[i+3] >= '0' && jsonStr[i+3] <= '9' &&
			jsonStr[i+4] >= '0' && jsonStr[i+4] <= '9' {
			return fmt.Errorf("JSON numbers cannot contain thousand separator comma, found: %s", jsonStr[i:min(i+10, len(jsonStr))])
		}
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func removeInvisibleRunes(s string) string {
	return reInvisibleRunes.ReplaceAllString(s, "")
}

func compactArrayOpen(s string) string {
	return reArrayOpenSpace.ReplaceAllString(strings.TrimSpace(s), "[{")
}
