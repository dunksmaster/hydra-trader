package trader

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"nofx/events"
	hlprovider "nofx/provider/hyperliquid"
)

const (
	copyFailTelegramBurstMax    = 5
	copyFailTelegramBurstWindow = 30 * time.Minute
	copyFailIncidentWindow      = 30 * time.Minute
)

var (
	copyFailAlertMu    sync.Mutex
	copyFailBurst      copyFailBurstState
	copyFailIncidentAt = map[string]time.Time{}
)

type copyFailBurstState struct {
	windowStart time.Time
	sent        int
}

func copyFailIncidentKey(traderID, symbol, action string) string {
	return strings.TrimSpace(traderID) + ":" +
		strings.ToUpper(strings.TrimSpace(symbol)) + ":" +
		strings.ToLower(strings.TrimSpace(action))
}

func copyFailDedupeKey(traderID, symbol, action string) string {
	return fmt.Sprintf("%s:%s:%s:%s", traderID, events.AlertCopyFailed, strings.ToUpper(strings.TrimSpace(symbol)), strings.ToLower(strings.TrimSpace(action)))
}

// shouldEmitCopyFailAlert suppresses transient HL state errors and caps repeated
// Telegram failure alerts to three per 30 minutes globally, one per symbol/action per bot.
func shouldEmitCopyFailAlert(traderID, symbol, action string, err error, context ...string) bool {
	if err != nil && isTransientCopyError(err) {
		return false
	}
	for _, s := range context {
		if events.IsTransientCopyFailureText(s) {
			return false
		}
	}
	copyFailAlertMu.Lock()
	defer copyFailAlertMu.Unlock()

	now := time.Now()
	incident := copyFailIncidentKey(traderID, symbol, action)
	if last, ok := copyFailIncidentAt[incident]; ok && now.Sub(last) < copyFailIncidentWindow {
		return false
	}
	if copyFailBurst.windowStart.IsZero() || now.Sub(copyFailBurst.windowStart) >= copyFailTelegramBurstWindow {
		copyFailBurst = copyFailBurstState{windowStart: now}
	}
	if copyFailBurst.sent >= copyFailTelegramBurstMax {
		return false
	}
	copyFailBurst.sent++
	copyFailIncidentAt[incident] = now
	return true
}

func (at *AutoTrader) copyFailAlertsSuppressed() bool {
	if at.config.StrategyConfig != nil && suppressCopyNoiseAlerts(at.config.StrategyConfig.CopyConfig) {
		return true
	}
	at.isRunningMutex.RLock()
	running := at.isRunning
	at.isRunningMutex.RUnlock()
	return !running
}

func (at *AutoTrader) emitCopyFailAlert(fill hlprovider.LeaderFill, symbol, action string, err error) {
	if at.copyFailAlertsSuppressed() {
		return
	}
	if err != nil && isTransientCopyError(err) {
		return
	}
	if !shouldEmitCopyFailAlert(at.id, symbol, action, err) {
		return
	}
	leader := ""
	if at.config.StrategyConfig.CopyConfig != nil {
		leader = at.config.StrategyConfig.CopyConfig.LeaderAddress
	}
	msg := fmt.Sprintf("Leader %s → %s %s failed: %v", shortCopyAddr(leader), symbol, action, err)
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertCopyFailed,
		Message:    msg,
		DedupeKey:  copyFailDedupeKey(at.id, symbol, action),
	})
}

func (at *AutoTrader) emitCopyFailAlertLeg(symbol, action, reason string, err error) {
	if at.copyFailAlertsSuppressed() {
		return
	}
	if err != nil && isTransientCopyError(err) {
		return
	}
	if events.IsTransientCopyFailureText(reason) {
		return
	}
	if !shouldEmitCopyFailAlert(at.id, symbol, action, err, reason) {
		return
	}
	msg := fmt.Sprintf("%s %s failed: %v (%s)", symbol, action, err, reason)
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertCopyFailed,
		Message:    msg,
		DedupeKey:  copyFailDedupeKey(at.id, symbol, action),
	})
}
