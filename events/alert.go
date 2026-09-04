package events

import "time"

const (
	AlertSafeMode        = "safe_mode"
	AlertSafeModeOff     = "safe_mode_off"
	AlertWalletEmpty     = "wallet_empty"
	AlertQuotaExhausted  = "quota_exhausted"
	AlertRateLimited     = "rate_limited"
	AlertCopyMirror      = "copy_mirror"
	AlertCopySkipped     = "copy_skipped"
	AlertCopyFailed      = "copy_failed"
	AlertCopyOverflow    = "copy_overflow"
	AlertCopyLeaderRule  = "copy_leader_rule"
	AlertLiquidationRisk = "liquidation_risk"
	AlertCopyPaused      = "copy_paused"
	AlertCopyLossPause   = "copy_loss_pause"
	AlertCopyL2Evicted   = "copy_l2_evicted"
)

// SystemAlertEvent is a trader-health condition that should reach Telegram.
type SystemAlertEvent struct {
	TraderID   string
	TraderName string
	Type       string
	Message    string
	// DedupeKey overrides the default traderID:type dedupe key when set (e.g. per-fill tid).
	DedupeKey string
	At        time.Time
}

var systemAlertHandler func(SystemAlertEvent)

// OnSystemAlert registers a callback for system alerts. Only one handler is kept.
func OnSystemAlert(h func(SystemAlertEvent)) {
	systemAlertHandler = h
}

// EmitSystemAlert notifies the registered handler asynchronously.
func EmitSystemAlert(e SystemAlertEvent) {
	if systemAlertHandler == nil {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	go systemAlertHandler(e)
}
