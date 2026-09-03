package trader

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"nofx/events"
)

const copyLeaderRuleDedupeWindow = 30 * time.Minute

var (
	copyLeaderRuleMu sync.Mutex
	copyLeaderRuleAt = map[string]time.Time{}
)

func copyLeaderRuleDedupeKey(traderID, symbol, action string) string {
	return fmt.Sprintf("%s:%s:%s:%s",
		strings.TrimSpace(traderID),
		events.AlertCopyLeaderRule,
		strings.ToUpper(strings.TrimSpace(symbol)),
		strings.ToLower(strings.TrimSpace(action)),
	)
}

func shouldEmitCopyLeaderRuleAlert(traderID, symbol, action string) bool {
	key := copyLeaderRuleDedupeKey(traderID, symbol, action)
	now := time.Now()
	copyLeaderRuleMu.Lock()
	defer copyLeaderRuleMu.Unlock()
	if last, ok := copyLeaderRuleAt[key]; ok && now.Sub(last) < copyLeaderRuleDedupeWindow {
		return false
	}
	copyLeaderRuleAt[key] = now
	return true
}

func leaderRuleReasonText(reason string) string {
	r := strings.TrimSpace(reason)
	switch {
	case r == "":
		return "Leader closed/flipped"
	case strings.HasPrefix(r, "leader closed"):
		return "Leader closed position"
	case strings.HasPrefix(r, "leader now long"):
		return "Leader flipped to long"
	case strings.HasPrefix(r, "leader now short"):
		return "Leader flipped to short"
	case strings.Contains(r, "leader close"):
		return "Leader closed position"
	default:
		return "Leader closed/flipped"
	}
}

// emitCopyLeaderRuleAlert notifies why a copy close is running (leader rule, not SL/TP/manual).
func (at *AutoTrader) emitCopyLeaderRuleAlert(symbol, action, venue, reason string) {
	if at == nil || symbol == "" || action == "" {
		return
	}
	if !shouldEmitCopyLeaderRuleAlert(at.id, symbol, action) {
		return
	}
	headline := leaderRuleReasonText(reason)
	msg := fmt.Sprintf("%s (copy rule) — closing %s %s on %s — not SL/TP, not manual",
		headline, strings.ToUpper(symbol), strings.ToLower(action), venue)
	if detail := strings.TrimSpace(reason); detail != "" && !strings.HasPrefix(detail, "leader") {
		msg = fmt.Sprintf("%s — %s", msg, detail)
	}
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertCopyLeaderRule,
		Message:    msg,
		DedupeKey:  copyLeaderRuleDedupeKey(at.id, symbol, action),
	})
}

// emitCopyLeaderOverflowCloseAlert explains a Bitget overflow close triggered by leader exit.
func (at *AutoTrader) emitCopyLeaderOverflowCloseAlert(symbol, side, venueName string) {
	action := "close_long"
	if strings.EqualFold(side, "short") {
		action = "close_short"
	}
	if !shouldEmitCopyLeaderRuleAlert(at.id, symbol, action) {
		return
	}
	msg := fmt.Sprintf("Leader flat on %s — closing overflow %s on %s (copy rule) — not SL/TP, not manual",
		strings.ToUpper(symbol), strings.ToLower(side), venueName)
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertCopyLeaderRule,
		Message:    msg,
		DedupeKey:  copyLeaderRuleDedupeKey(at.id, symbol, action),
	})
}
