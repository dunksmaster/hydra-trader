package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	CopyProfileCurrent = "current"
	CopyProfileLayer1  = "layer1"

	systemKeyCopyProfile  = "copy_strategy_profile"
	systemKeyCopySnapshot = "copy_strategy_snapshot"
)

// CopyLeaderSnapshot captures one copy bot's layer settings for profile revert.
type CopyLeaderSnapshot struct {
	TraderID      string `json:"trader_id"`
	TraderName    string `json:"trader_name"`
	StrategyID    string `json:"strategy_id"`
	LeaderAddress string `json:"leader_address"`
	CopyLayer     int    `json:"copy_layer"`
	CopyPaused    bool   `json:"copy_paused"`
	IsRunning     bool   `json:"is_running"`
}

// CopyStrategySnapshot is stored on first switch away from the live five-leader layout.
type CopyStrategySnapshot struct {
	Profile string               `json:"profile"`
	Leaders []CopyLeaderSnapshot `json:"leaders"`
	TakenAt time.Time            `json:"taken_at"`
}

var layer1LeaderLayout = map[string]struct {
	Layer  int
	Paused bool
	Name   string
}{
	"0x364a45829e8ce2940d8cff911076d8dec40b2e73": {1, false, "Hyperdash 364a"},
	"0xb7e0b9fbc9479330d70bcc82a7d4325a20e8d1aa": {1, false, "Hyperdash b7e0"},
	"0xe2823659be02e0f48a4660e4da008b5e1abfdf29": {1, false, "Hyperdash e282"},
	"0x0ad9e656d9e6211d0ea1c5462342e1fc94cc4cbf": {2, false, "Leviathan"},
	"0x6a02aedceac5a6813d960e4dae1910d9c458e77c": {2, false, "Copy L4"},
	"0x8a0cd16a004e21e04936a0a01c6f9a49ff937914": {3, true, "Money Printer"},
	"0xdebbea84972174f44778a00521b1b5faa663abbb": {3, true, "Grinder"},
	"0x6859da14835424957a1e6b397d8026b1d9ff7e1e": {3, true, "Alpha 6859"},
	"0x020ca66c30bec2c4fe3861a94e4db4a498a35872": {1, false, "machibigbrother"},
}

// GetCopyStrategyProfile returns the active copy layout profile ("current" by default).
func (s *Store) GetCopyStrategyProfile() string {
	val, err := s.GetSystemConfig(systemKeyCopyProfile)
	if err != nil || strings.TrimSpace(val) == "" {
		return CopyProfileCurrent
	}
	return strings.TrimSpace(val)
}

// SetCopyStrategyProfile records the active profile name.
func (s *Store) SetCopyStrategyProfile(profile string) error {
	profile = strings.TrimSpace(strings.ToLower(profile))
	if profile == "" {
		profile = CopyProfileCurrent
	}
	return s.SetSystemConfig(systemKeyCopyProfile, profile)
}

func (s *Store) loadCopySnapshot() (*CopyStrategySnapshot, error) {
	raw, err := s.GetSystemConfig(systemKeyCopySnapshot)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var snap CopyStrategySnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func (s *Store) saveCopySnapshot(snap *CopyStrategySnapshot) error {
	if snap == nil {
		return s.SetSystemConfig(systemKeyCopySnapshot, "")
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return s.SetSystemConfig(systemKeyCopySnapshot, string(data))
}

// SnapshotCopyLeadersIfNeeded stores the current copy-bot layout once before the first layer1 switch.
func (s *Store) SnapshotCopyLeadersIfNeeded(userID string) error {
	existing, err := s.loadCopySnapshot()
	if err != nil {
		return err
	}
	if existing != nil && len(existing.Leaders) > 0 {
		return nil
	}
	leaders, err := s.listCopyLeaderSnapshots(userID)
	if err != nil {
		return err
	}
	if len(leaders) == 0 {
		return fmt.Errorf("no copy traders found to snapshot")
	}
	return s.saveCopySnapshot(&CopyStrategySnapshot{
		Profile: CopyProfileCurrent,
		Leaders: leaders,
		TakenAt: time.Now().UTC(),
	})
}

func (s *Store) listCopyLeaderSnapshots(userID string) ([]CopyLeaderSnapshot, error) {
	traders, err := s.Trader().List(userID)
	if err != nil {
		return nil, err
	}
	out := make([]CopyLeaderSnapshot, 0, 8)
	for _, tr := range traders {
		if tr.StrategyID == "" {
			continue
		}
		st, err := s.Strategy().Get(userID, tr.StrategyID)
		if err != nil {
			continue
		}
		cfg, err := st.ParseConfig()
		if err != nil || cfg.StrategyType != "copy_trading" || cfg.CopyConfig == nil {
			continue
		}
		cc := cfg.CopyConfig
		cc.Normalize()
		out = append(out, CopyLeaderSnapshot{
			TraderID:      tr.ID,
			TraderName:    tr.Name,
			StrategyID:    st.ID,
			LeaderAddress: cc.LeaderAddress,
			CopyLayer:     cc.CopyLayer,
			CopyPaused:    cc.CopyPaused,
			IsRunning:     tr.IsRunning,
		})
	}
	return out, nil
}

func (s *Store) updateCopyStrategyLayer(userID, strategyID string, layer int, paused bool) error {
	st, err := s.Strategy().Get(userID, strategyID)
	if err != nil {
		return err
	}
	cfg, err := st.ParseConfig()
	if err != nil || cfg.CopyConfig == nil {
		return fmt.Errorf("strategy %s is not copy_trading", strategyID)
	}
	cfg.CopyConfig.CopyLayer = layer
	cfg.CopyConfig.CopyPaused = paused
	cfg.CopyConfig.Normalize()
	if err := st.SetConfig(cfg); err != nil {
		return err
	}
	st.UpdatedAt = time.Now().UTC()
	return s.Strategy().Update(st)
}

// ApplyCopyStrategyProfile sets copy_layer/copy_paused on strategies for the requested profile.
func (s *Store) ApplyCopyStrategyProfile(userID, profile string) (string, error) {
	profile = strings.TrimSpace(strings.ToLower(profile))
	switch profile {
	case CopyProfileLayer1:
		if err := s.SnapshotCopyLeadersIfNeeded(userID); err != nil {
			return "", err
		}
		traders, err := s.Trader().List(userID)
		if err != nil {
			return "", err
		}
		updated := 0
		for _, tr := range traders {
			if tr.StrategyID == "" {
				continue
			}
			st, err := s.Strategy().Get(userID, tr.StrategyID)
			if err != nil {
				continue
			}
			cfg, err := st.ParseConfig()
			if err != nil || cfg.CopyConfig == nil {
				continue
			}
			leader := strings.ToLower(strings.TrimSpace(cfg.CopyConfig.LeaderAddress))
			layout, ok := layer1LeaderLayout[leader]
			if !ok {
				continue
			}
			if err := s.updateCopyStrategyLayer(userID, st.ID, layout.Layer, layout.Paused); err != nil {
				return "", err
			}
			updated++
		}
		if err := s.SetCopyStrategyProfile(CopyProfileLayer1); err != nil {
			return "", err
		}
		return fmt.Sprintf("Layer1 profile applied (%d strategies updated). Run ops_apply_layer1.go if L1 bots are missing.", updated), nil

	case CopyProfileCurrent:
		snap, err := s.loadCopySnapshot()
		if err != nil {
			return "", err
		}
		if snap == nil || len(snap.Leaders) == 0 {
			if err := s.SetCopyStrategyProfile(CopyProfileCurrent); err != nil {
				return "", err
			}
			return "Profile set to current (no snapshot to restore — layer fields unchanged).", nil
		}
		for _, leader := range snap.Leaders {
			if leader.StrategyID == "" {
				continue
			}
			layer := leader.CopyLayer
			if layer <= 0 {
				layer = 2
			}
			if err := s.updateCopyStrategyLayer(userID, leader.StrategyID, layer, leader.CopyPaused); err != nil {
				return "", err
			}
		}
		if err := s.SetCopyStrategyProfile(CopyProfileCurrent); err != nil {
			return "", err
		}
		return fmt.Sprintf("Restored current profile (%d leaders from snapshot).", len(snap.Leaders)), nil

	default:
		return "", fmt.Errorf("unknown profile %q (use current or layer1)", profile)
	}
}
