package trader

import (
	"nofx/kernel"

	"nofx/store"

	"testing"
)

func TestComputeCopyNotionalFixed(t *testing.T) {

	cfg := store.DefaultCopyStrategyConfig()

	cfg.SizeMode = "fixed_notional"

	cfg.NotionalUSD = 15

	notional, reason := ComputeCopyNotionalUSD(&cfg, 500_000, 2_400_000, 47, 47)

	if reason != "" {

		t.Fatalf("unexpected skip: %s", reason)

	}

	if notional != 15 {

		t.Fatalf("fixed notional = %.2f, want 15", notional)

	}

}

func TestComputeCopyNotionalProportionalBelowMin(t *testing.T) {

	cfg := store.DefaultCopyStrategyConfig()

	cfg.SizeMode = "proportional"

	cfg.MinNotionalUSD = 12

	notional, reason := ComputeCopyNotionalUSD(&cfg, 500_000, 2_400_000, 47, 47)

	if reason == "" {

		t.Fatalf("expected skip below minimum, got notional %.2f", notional)

	}

}

func TestComputeCopyNotionalProportionalCapped(t *testing.T) {

	cfg := store.DefaultCopyStrategyConfig()

	cfg.SizeMode = "proportional"

	cfg.CopyRatio = 1

	cfg.MinNotionalUSD = 5

	cfg.MaxNotionalPct = 40

	notional, reason := ComputeCopyNotionalUSD(&cfg, 10_000, 100, 100, 100)

	if reason != "" {

		t.Fatalf("unexpected skip: %s", reason)

	}

	if notional != 40 {

		t.Fatalf("capped notional = %.2f, want 40", notional)

	}

}

func TestComputeCopyNotionalMarginCap(t *testing.T) {

	cfg := store.DefaultCopyStrategyConfig()

	cfg.NotionalUSD = 15

	cfg.MaxNotionalPct = 45

	cfg.MaxLeverage = 10

	cfg.MaxPositions = 2

	notional, reason := ComputeCopyNotionalUSD(&cfg, 500_000, 2_400_000, 47, 47)

	if reason != "" {

		t.Fatalf("unexpected skip: %s", reason)

	}

	// (47/2) * 10 * 0.85 = 199.75, equity cap = 47*0.45 = 21.15

	if notional > 21.16 || notional < 12 {

		t.Fatalf("adaptive notional = %.2f, want ~12-21", notional)

	}

}

func TestComputeCopyNotionalWalletCopySlots(t *testing.T) {
	cfg := store.DefaultCopyStrategyConfig()
	cfg.NotionalUSD = 300
	cfg.MaxLeverage = 10
	cfg.MaxPositions = 1
	cfg.MaxNotionalPct = 0
	cfg.WalletCopySlots = 3

	notional, reason := ComputeCopyNotionalUSD(&cfg, 500_000, 2_400_000, 1000, 90)
	if reason != "" {
		t.Fatalf("unexpected skip: %s", reason)
	}
	// (90/3) * 10 * 0.85 = 255
	if notional > 255.01 || notional < 254 {
		t.Fatalf("wallet_copy_slots notional = %.2f, want ~255", notional)
	}

	cfg.WalletCopySlots = 0
	notional2, reason := ComputeCopyNotionalUSD(&cfg, 500_000, 2_400_000, 1000, 90)
	if reason != "" {
		t.Fatalf("unexpected skip: %s", reason)
	}
	if notional2 != 300 {
		t.Fatalf("default slots notional = %.2f, want 300", notional2)
	}
}

func TestDiffCopyLegsCloseAndOpen(t *testing.T) {

	targets := map[copyLegKey]copyLeg{

		{Symbol: "HYPEUSDT", Side: "long"}: {

			Symbol: "HYPEUSDT", Side: "long", NotionalUSD: 15, Leverage: 5, LeaderNotionalUSD: 1000,
		},
	}

	follower := map[copyLegKey]copyLeg{

		{Symbol: "BTCUSDT", Side: "short"}: {Symbol: "BTCUSDT", Side: "short"},

		{Symbol: "HYPEUSDT", Side: "short"}: {Symbol: "HYPEUSDT", Side: "short"},
	}

	closes, opens := diffCopyLegs(targets, follower, 2, 5, 2)

	if len(closes) != 1 {
		t.Fatalf("closes = %+v, side flip must enqueue exactly one close", closes)
	}

	if len(opens) != 1 || opens[0].Symbol != "HYPEUSDT" || opens[0].Side != "long" {

		t.Fatalf("opens = %+v, want one HYPE long open", opens)

	}

	foundHypeShort := false

	for _, c := range closes {

		if c.Symbol == "BTCUSDT" {

			t.Fatalf("closes = %+v, BTC leg must be ignored (other copy bot)", closes)

		}

		if c.Symbol == "HYPEUSDT" && c.Side == "short" {

			foundHypeShort = true

		}

	}

	if !foundHypeShort {

		t.Fatalf("closes = %+v, want HYPE short close", closes)

	}

}

func TestDiffCopyLegsIgnoresUnrelatedSymbols(t *testing.T) {

	targets := map[copyLegKey]copyLeg{

		{Symbol: "ETHUSDT", Side: "short"}: {

			Symbol: "ETHUSDT", Side: "short", NotionalUSD: 50, LeaderNotionalUSD: 1000,
		},
	}

	follower := map[copyLegKey]copyLeg{

		{Symbol: "BTCUSDT", Side: "long"}: {Symbol: "BTCUSDT", Side: "long"},
	}

	closes, opens := diffCopyLegs(targets, follower, 1, 5, 1)

	if len(closes) != 0 {

		t.Fatalf("closes = %+v, want none (BTC belongs to another leader bot)", closes)

	}

	if len(opens) != 1 || opens[0].Symbol != "ETHUSDT" {

		t.Fatalf("opens = %+v, want ETH short open", opens)

	}

}

func TestDiffCopyLegsPrioritizeByLeaderNotional(t *testing.T) {

	targets := map[copyLegKey]copyLeg{

		{Symbol: "AAAUSDT", Side: "long"}: {

			Symbol: "AAAUSDT", Side: "long", NotionalUSD: 15, LeaderNotionalUSD: 100,
		},

		{Symbol: "BBBUSDT", Side: "short"}: {

			Symbol: "BBBUSDT", Side: "short", NotionalUSD: 15, LeaderNotionalUSD: 500_000,
		},

		{Symbol: "CCCUSDT", Side: "long"}: {

			Symbol: "CCCUSDT", Side: "long", NotionalUSD: 15, LeaderNotionalUSD: 50_000,
		},
	}

	closes, opens := diffCopyLegs(targets, map[copyLegKey]copyLeg{}, 0, 5, 2)

	if len(opens) != 2 {

		t.Fatalf("opens = %+v, want 2", opens)

	}

	if opens[0].Symbol != "BBBUSDT" || opens[1].Symbol != "CCCUSDT" {

		t.Fatalf("opens order = %s then %s, want BBB then CCC", opens[0].Symbol, opens[1].Symbol)

	}

	if len(closes) != 0 {

		t.Fatalf("closes = %+v, want none", closes)

	}

}

func TestIsCopySymbolBlocked(t *testing.T) {

	cfg := store.DefaultCopyStrategyConfig()

	cfg.SymbolBlocklist = []string{"xyz:"}

	if !isCopySymbolBlocked(&cfg, "xyz:TSLA", "XYZ:TSLA") {

		t.Fatal("expected xyz symbol to be blocked")

	}

	if isCopySymbolBlocked(&cfg, "HYPE", "HYPEUSDT") {

		t.Fatal("did not expect HYPE to be blocked")

	}

	if isCopySymbolBlocked(&store.CopyStrategyConfig{}, "xyz:CRCL", "XYZ:CRCL") {

		t.Fatal("empty blocklist should allow xyz")

	}

}

func TestApplyCopyProtectivePricesLeaderPlusStop(t *testing.T) {

	cfg := store.DefaultCopyStrategyConfig()

	decision := &kernel.Decision{Action: "open_long", Leverage: 5}

	applyCopyProtectivePrices(decision, 100, &cfg)

	if decision.StopLoss <= 0 || decision.TakeProfit <= 0 {

		t.Fatalf("expected protective prices, got sl=%f tp=%f", decision.StopLoss, decision.TakeProfit)

	}

	if decision.StopLoss >= 100 || decision.TakeProfit <= 100 {

		t.Fatalf("long protective prices look wrong: sl=%f tp=%f", decision.StopLoss, decision.TakeProfit)

	}

}
