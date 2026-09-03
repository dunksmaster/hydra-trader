package trader

import (
	"strings"
	"time"

	"nofx/events"
	"nofx/kernel"
	"nofx/store"
	"nofx/trader/hyperliquid"
)

func isCopyOrderRecoverableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid price") ||
		strings.Contains(msg, "invalid size") ||
		strings.Contains(msg, "float_to_wire")
}

func isTransientCopyError(err error) bool {
	if err == nil {
		return false
	}
	if hyperliquid.IsTransientAPIError(err) {
		return true
	}
	return events.IsTransientCopyFailureText(err.Error())
}

// executeCopyDecisionWithRetry mirrors leader actions. Upstream state failures
// are not retried immediately because doing so amplifies Hyperliquid rate
// limiting; the copy loop's circuit breaker schedules a later attempt.
func (at *AutoTrader) executeCopyDecisionWithRetry(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 700 * time.Millisecond)
			at.logInfof("[Copy] retry %d for %s %s after: %v", attempt+1, decision.Symbol, decision.Action, lastErr)
		}
		lastErr = at.executeDecisionWithRecord(decision, actionRecord)
		if lastErr == nil {
			return nil
		}
		if isTransientCopyError(lastErr) {
			break
		}
		if strings.HasPrefix(decision.Action, "close_") && isCopyOrderRecoverableError(lastErr) {
			at.logInfof("[Copy] close retry (close-all) after recoverable error: %v", lastErr)
			retryDecision := *decision
			retryDecision.Quantity = 0
			retryRecord := *actionRecord
			retryRecord.Reasoning = actionRecord.Reasoning + " | retry close-all"
			if retryErr := at.executeDecisionWithRecord(&retryDecision, &retryRecord); retryErr == nil {
				*actionRecord = retryRecord
				return nil
			}
		}
		if !isTransientCopyError(lastErr) {
			break
		}
	}
	return lastErr
}
