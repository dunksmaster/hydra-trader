package store

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

const OverflowLegOpen = "open"
const OverflowLegClosed = "closed"

// CopyOverflowLeg is a venue fill opened because Hyperliquid skipped.
type CopyOverflowLeg struct {
	ID               int64     `gorm:"primaryKey;autoIncrement"`
	SourceTraderID   string    `gorm:"column:source_trader_id;not null;index:idx_overflow_source"`
	OverflowTraderID string    `gorm:"column:overflow_trader_id;not null;index:idx_overflow_target"`
	LeaderAddress    string    `gorm:"column:leader_address;not null;index:idx_overflow_leader_sym"`
	Symbol           string    `gorm:"column:symbol;not null;index:idx_overflow_leader_sym"`
	Side             string    `gorm:"column:side;not null"`
	Quantity         float64   `gorm:"column:quantity;default:0"`
	OpenTid          int64     `gorm:"column:open_tid;default:0"`
	Status           string    `gorm:"column:status;not null;index:idx_overflow_status"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at"`
}

func (CopyOverflowLeg) TableName() string { return "copy_overflow_legs" }

type CopyOverflowStore struct {
	db *gorm.DB
}

func NewCopyOverflowStore(db *gorm.DB) *CopyOverflowStore {
	return &CopyOverflowStore{db: db}
}

func (s *CopyOverflowStore) initTables() error {
	return s.db.AutoMigrate(&CopyOverflowLeg{})
}

func normalizeOverflowSide(side string) string {
	s := strings.ToLower(strings.TrimSpace(side))
	if s == "short" {
		return "short"
	}
	return "long"
}

func (s *CopyOverflowStore) FindOpen(overflowTraderID, leader, symbol, side string) (*CopyOverflowLeg, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var row CopyOverflowLeg
	err := s.db.Where(
		"overflow_trader_id = ? AND leader_address = ? AND symbol = ? AND side = ? AND status = ?",
		overflowTraderID, strings.ToLower(strings.TrimSpace(leader)), strings.TrimSpace(symbol), normalizeOverflowSide(side), OverflowLegOpen,
	).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *CopyOverflowStore) HasOpenOnVenue(overflowTraderID, symbol, side string) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	var n int64
	err := s.db.Model(&CopyOverflowLeg{}).Where(
		"overflow_trader_id = ? AND symbol = ? AND side = ? AND status = ?",
		overflowTraderID, strings.TrimSpace(symbol), normalizeOverflowSide(side), OverflowLegOpen,
	).Count(&n).Error
	return n > 0, err
}

func (s *CopyOverflowStore) OpenSides(overflowTraderID string) (map[string]string, error) {
	out := map[string]string{}
	if s == nil || s.db == nil {
		return out, nil
	}
	var rows []CopyOverflowLeg
	if err := s.db.Where("overflow_trader_id = ? AND status = ?", overflowTraderID, OverflowLegOpen).Find(&rows).Error; err != nil {
		return out, err
	}
	for _, row := range rows {
		out[row.Symbol] = row.Side
	}
	return out, nil
}

func (s *CopyOverflowStore) InsertOpen(leg CopyOverflowLeg) error {
	if s == nil || s.db == nil {
		return nil
	}
	now := time.Now().UTC()
	leg.LeaderAddress = strings.ToLower(strings.TrimSpace(leg.LeaderAddress))
	leg.Symbol = strings.TrimSpace(leg.Symbol)
	leg.Side = normalizeOverflowSide(leg.Side)
	leg.Status = OverflowLegOpen
	leg.CreatedAt = now
	leg.UpdatedAt = now
	return s.db.Create(&leg).Error
}

func (s *CopyOverflowStore) MarkClosed(id int64) error {
	if s == nil || s.db == nil || id == 0 {
		return nil
	}
	return s.db.Model(&CopyOverflowLeg{}).Where("id = ?", id).Updates(map[string]any{
		"status":     OverflowLegClosed,
		"updated_at": time.Now().UTC(),
	}).Error
}

func (s *CopyOverflowStore) ListOpenForOverflowTrader(overflowTraderID string) ([]CopyOverflowLeg, error) {
	if s == nil || s.db == nil || overflowTraderID == "" {
		return nil, nil
	}
	var rows []CopyOverflowLeg
	err := s.db.Where("overflow_trader_id = ? AND status = ?", overflowTraderID, OverflowLegOpen).
		Order("created_at ASC").Find(&rows).Error
	return rows, err
}
