package kernel

import "testing"

func TestSanitizeDecisionJSONStripsEllipsis(t *testing.T) {
	got := sanitizeDecisionJSON(`[{"symbol": "", "action": "wait", ...}]`)
	if got != `[{"symbol": "", "action": "wait"}]` {
		t.Fatalf("got %q", got)
	}
}

func TestExtractDecisionsUsesLastDecisionTag(t *testing.T) {
	raw := `
<decision>
[{"symbol": "", "action": "wait", ...}]
</decision>
<reasoning>at max positions</reasoning>
<decision>
[]
</decision>`
	decs, err := extractDecisions(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(decs) != 1 || decs[0].Action != "wait" {
		t.Fatalf("got %+v", decs)
	}
}

func TestExtractDecisionsEllipsisDraftIsWait(t *testing.T) {
	raw := `<decision>[{"symbol": "", "action": "wait", ...}]</decision>`
	decs, err := extractDecisions(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(decs) != 1 || decs[0].Action != "wait" || decs[0].Symbol != "ALL" {
		t.Fatalf("got %+v", decs)
	}
}

func TestExtractDecisionsEmptyArrayIsWait(t *testing.T) {
	decs, err := extractDecisions(`<decision>[]</decision>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(decs) != 1 || decs[0].Action != "wait" {
		t.Fatalf("got %+v", decs)
	}
}

func TestExtractDecisionsStillParsesRealOpen(t *testing.T) {
	raw := `<decision>[{"symbol":"BTCUSDT","action":"open_short","leverage":5,"confidence":80}]</decision>`
	decs, err := extractDecisions(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(decs) != 1 || decs[0].Symbol != "BTCUSDT" || decs[0].Action != "open_short" {
		t.Fatalf("got %+v", decs)
	}
}
