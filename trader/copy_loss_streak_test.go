package trader

import (
	"testing"

	"nofx/store"
)

func TestApplyCopyLossClosePausesAtFive(t *testing.T) {
	cc := &store.CopyStrategyConfig{
		CopyLayer:       2,
		CopyPaused:      false,
		PauseLossStreak: 5,
	}
	for i := 1; i <= 4; i++ {
		pause, changed := applyCopyLossClose(cc, -0.1)
		if pause || !changed {
			t.Fatalf("loss %d: pause=%v changed=%v", i, pause, changed)
		}
		if cc.LossStreak != i {
			t.Fatalf("loss %d: streak=%d", i, cc.LossStreak)
		}
	}
	pause, changed := applyCopyLossClose(cc, -0.05)
	if !pause || !changed {
		t.Fatalf("5th loss: pause=%v changed=%v", pause, changed)
	}
	if !cc.CopyPaused || cc.CopyLayer != 3 || cc.LossStreak != 5 {
		t.Fatalf("after pause: paused=%v layer=%d streak=%d", cc.CopyPaused, cc.CopyLayer, cc.LossStreak)
	}
}

func TestApplyCopyLossCloseWinResetsButStaysPaused(t *testing.T) {
	cc := &store.CopyStrategyConfig{
		CopyLayer:       3,
		CopyPaused:      true,
		LossStreak:      5,
		PauseLossStreak: 5,
	}
	pause, changed := applyCopyLossClose(cc, 0.42)
	if pause || !changed {
		t.Fatalf("win: pause=%v changed=%v", pause, changed)
	}
	if cc.LossStreak != 0 || !cc.CopyPaused {
		t.Fatalf("streak=%d paused=%v", cc.LossStreak, cc.CopyPaused)
	}
}

func TestApplyCopyLossCloseBreakevenCountsAsLoss(t *testing.T) {
	cc := &store.CopyStrategyConfig{PauseLossStreak: 3, LossStreak: 1}
	pause, changed := applyCopyLossClose(cc, 0)
	if pause || !changed || cc.LossStreak != 2 {
		t.Fatalf("breakeven: pause=%v changed=%v streak=%d", pause, changed, cc.LossStreak)
	}
}

func TestApplyCopyLossCloseDisabled(t *testing.T) {
	cc := &store.CopyStrategyConfig{PauseLossStreak: 0}
	pause, changed := applyCopyLossClose(cc, -1)
	if pause || changed || cc.LossStreak != 0 {
		t.Fatalf("disabled: pause=%v changed=%v streak=%d", pause, changed, cc.LossStreak)
	}
}
