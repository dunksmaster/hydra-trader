package trader

import (
	"testing"
)

func TestDeterministicEvictSameCoinOppositeSide(t *testing.T) {
	l1Side := "long"
	candidates := []l2EvictCandidate{
		{Symbol: "BTCUSDT", Side: "long", PnLPct: 5.0, TraderName: "L2-A"},
		{Symbol: "BTCUSDT", Side: "short", PnLPct: -1.0, TraderName: "L2-B"},
		{Symbol: "ETHUSDT", Side: "long", PnLPct: 2.0, TraderName: "L2-C"},
	}
	picked := PickL2EvictCandidateForTest(candidates, "BTCUSDT", l1Side)
	if picked == nil {
		t.Fatal("expected a candidate")
	}
	if picked.Symbol != "BTCUSDT" || picked.Side != "short" {
		t.Fatalf("want BTC short opposite-side first, got %s %s", picked.Symbol, picked.Side)
	}
}

func TestDeterministicEvictProtectLargeRunner(t *testing.T) {
	runner := l2EvictCandidate{Symbol: "SOLUSDT", Side: "long", PnLPct: 4.0}
	all := []l2EvictCandidate{
		runner,
		{Symbol: "DOGEUSDT", Side: "long", PnLPct: 0.5},
	}
	scoreRunner := DeterministicEvictScoreForTest(runner, "HYPEUSDT", "long", all)
	scoreSmall := DeterministicEvictScoreForTest(all[1], "HYPEUSDT", "long", all)
	if scoreRunner <= scoreSmall {
		t.Fatalf("large runner should rank later: runner=%.1f small=%.1f", scoreRunner, scoreSmall)
	}
	picked := PickL2EvictCandidateForTest(all, "HYPEUSDT", "long")
	if picked.Symbol != "DOGEUSDT" {
		t.Fatalf("expected smaller winner evicted first, got %s", picked.Symbol)
	}
}

func TestDeterministicEvictBankWinnerBeforeLoser(t *testing.T) {
	candidates := []l2EvictCandidate{
		{Symbol: "AAVEUSDT", Side: "long", PnLPct: -2.0},
		{Symbol: "LINKUSDT", Side: "long", PnLPct: 1.5},
	}
	picked := PickL2EvictCandidateForTest(candidates, "XRPUSDT", "long")
	if picked.PnLPct <= 0 {
		t.Fatalf("expected winner banked before loser, got pnl %.2f", picked.PnLPct)
	}
}

func TestCountAllWalletLegs(t *testing.T) {
	positions := []map[string]interface{}{
		{"symbol": "BTCUSDT", "side": "long", "positionAmt": 0.01},
		{"symbol": "ETHUSDT", "side": "short", "positionAmt": 0.0},
		{"symbol": "SOLUSDT", "side": "long", "positionAmt": 1.5},
	}
	if n := countAllWalletLegs(positions); n != 2 {
		t.Fatalf("countAllWalletLegs = %d, want 2", n)
	}
}

func TestParseLayer1EvictDecision(t *testing.T) {
	raw := `Here is my advice: {"action":"wait","reason":"weak L1 fill"}`
	d := parseLayer1EvictDecision(raw)
	if d == nil || d.Action != "wait" {
		t.Fatalf("parse failed: %+v", d)
	}
}

func TestSelectEvictCandidateFailsClosed(t *testing.T) {
	candidates := []l2EvictCandidate{
		{Symbol: "BTCUSDT", Side: "long", PnLPct: 1.0, TraderName: "L2-A"},
		{Symbol: "ETHUSDT", Side: "short", PnLPct: -2.0, TraderName: "L2-B"},
	}
	cases := []struct {
		name     string
		decision *layer1EvictDecision
	}{
		{"nil decision (model error, timeout or unparseable)", nil},
		{"empty action (truncated response)", &layer1EvictDecision{}},
		{"wait", &layer1EvictDecision{Action: "wait"}},
		{"hold", &layer1EvictDecision{Action: "hold", Symbol: "BTCUSDT"}},
		{"none", &layer1EvictDecision{Action: "none", Symbol: "BTCUSDT"}},
		{"close with no target named", &layer1EvictDecision{Action: "close"}},
		{"close naming an unknown symbol", &layer1EvictDecision{Action: "close", Symbol: "DOGEUSDT"}},
		{"close naming a side we do not hold", &layer1EvictDecision{Action: "close", Symbol: "BTCUSDT", Side: "short"}},
		{"pick_other naming an unknown trader", &layer1EvictDecision{Action: "pick_other", TraderID: "ghost"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := SelectEvictCandidateForTest(tc.decision, candidates)
			if got != nil {
				t.Fatalf("must not evict, got %s %s", got.Symbol, got.Side)
			}
			if reason == "" {
				t.Fatal("expected a skip reason for the log")
			}
		})
	}
}

func TestResolveL2EvictCandidateNilUsesDeterministic(t *testing.T) {
	candidates := []l2EvictCandidate{
		{Symbol: "BTCUSDT", Side: "long", PnLPct: 5.0, TraderName: "L2-A"},
		{Symbol: "BTCUSDT", Side: "short", PnLPct: -1.0, TraderName: "L2-B"},
	}
	got := resolveL2EvictCandidate(nil, candidates, "BTCUSDT", "long", nil)
	if got == nil {
		t.Fatal("expected deterministic fallback")
	}
	if got.Symbol != "BTCUSDT" || got.Side != "short" {
		t.Fatalf("want BTC short, got %s %s", got.Symbol, got.Side)
	}
}

func TestResolveL2EvictCandidateWaitNoEvict(t *testing.T) {
	candidates := []l2EvictCandidate{
		{Symbol: "BTCUSDT", Side: "long", PnLPct: 1.0},
	}
	got := resolveL2EvictCandidate(&layer1EvictDecision{Action: "wait"}, candidates, "ETHUSDT", "long", nil)
	if got != nil {
		t.Fatalf("wait must not evict, got %s", got.Symbol)
	}
}

func TestResolveL2EvictCandidateUnmatchedCloseUsesDeterministic(t *testing.T) {
	candidates := []l2EvictCandidate{
		{Symbol: "LINKUSDT", Side: "long", PnLPct: 1.5},
		{Symbol: "AAVEUSDT", Side: "long", PnLPct: -2.0},
	}
	got := resolveL2EvictCandidate(&layer1EvictDecision{Action: "close", Symbol: "DOGEUSDT"}, candidates, "XRPUSDT", "long", nil)
	if got == nil || got.Symbol != "LINKUSDT" {
		t.Fatalf("expected deterministic winner, got %+v", got)
	}
}

func TestSelectEvictCandidateAcceptsExplicitClose(t *testing.T) {
	candidates := []l2EvictCandidate{
		{Symbol: "BTCUSDT", Side: "long", PnLPct: 1.0},
		{Symbol: "ETHUSDT", Side: "short", PnLPct: -2.0},
	}
	got, reason := SelectEvictCandidateForTest(&layer1EvictDecision{
		Action: "close", Symbol: "ETHUSDT", Side: "short",
	}, candidates)
	if got == nil {
		t.Fatalf("expected an eviction, skipped: %s", reason)
	}
	if got.Symbol != "ETHUSDT" || got.Side != "short" {
		t.Fatalf("evicted %s %s, want ETHUSDT short", got.Symbol, got.Side)
	}
}
