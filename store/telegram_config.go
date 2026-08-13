package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// TelegramConfig stores the Telegram bot binding (single row, always ID=1)
type TelegramConfig struct {
	ID        uint      `gorm:"primaryKey"`
	BotToken  string    `gorm:"column:bot_token"`
	ChatID    int64     `gorm:"column:chat_id"`
	Username  string    `gorm:"column:username"` // @username for display
	BoundAt   time.Time `gorm:"column:bound_at"`
	ModelID   string    `gorm:"column:model_id;default:''"` // AI model used for Telegram replies
	Language  string    `gorm:"column:language;default:''"` // "zh" or "en"; empty = not chosen yet
	BindCode          string  `gorm:"column:bind_code;default:''"`           // One-time code required to bind via /start
	NotifyEnabled     bool    `gorm:"column:notify_enabled;default:true"`    // Push trade alerts to bound chat
	DigestEnabled     bool    `gorm:"column:digest_enabled;default:true"`    // Daily snapshot + swing alerts
	PnlSwingThreshold float64 `gorm:"column:pnl_swing_threshold;default:5"`  // uPnL move threshold (USD)
	SelectedTraderID  string  `gorm:"column:selected_trader_id;default:''"`  // "" or "*" = all; else one trader_id
	CreatedAt time.Time
	UpdatedAt time.Time
}

// String returns a safe string representation of TelegramConfig with the token masked.
func (tc TelegramConfig) String() string {
	token := "***"
	if tc.BotToken == "" {
		token = "<not set>"
	}
	return fmt.Sprintf("TelegramConfig{ID:%d, ChatID:%d, Username:%q, BotToken:%s, BoundAt:%v}",
		tc.ID, tc.ChatID, tc.Username, token, tc.BoundAt)
}

// TelegramConfigStore defines the interface for Telegram bot binding operations
type TelegramConfigStore interface {
	Get() (*TelegramConfig, error)                    // Get current config (may not exist)
	SaveToken(botToken string) error                  // Save bot token only (Web UI sets this)
	Save(botToken, modelID string) error              // Save bot token + selected AI model
	BindUser(chatID int64, username string, bindCode string) error // Called on first /start with dashboard code
	IsBound() (bool, error)                           // Check if any user is bound
	GetBoundChatID() (int64, error)                   // Get bound chat ID (0 if not bound)
	EnsureBindCode() (string, error)                  // Generate bind code when unbound
	Unbind() error                                    // Remove binding
	SetLanguage(lang string) error                    // Set UI language ("en" or "zh")
	GetLanguage() string                              // Get UI language; returns "en" if not set
	SetNotifyEnabled(enabled bool) error              // Enable/disable proactive trade alerts
	IsNotifyEnabled() bool                            // Whether trade alerts are enabled (default true)
	SetDigestEnabled(enabled bool) error
	IsDigestEnabled() bool
	GetPnlSwingThreshold() float64
	SetPnlSwingThreshold(threshold float64) error
	GetSelectedTraderID() string
	SetSelectedTraderID(traderID string) error
}

type telegramConfigStore struct {
	db *gorm.DB
	mu sync.RWMutex
}

// NewTelegramConfigStore creates a new TelegramConfigStore
func NewTelegramConfigStore(db *gorm.DB) TelegramConfigStore {
	return &telegramConfigStore{db: db}
}

func (s *telegramConfigStore) initTables() error {
	if err := s.db.AutoMigrate(&TelegramConfig{}); err != nil {
		return err
	}
	// SQLite adds notify_enabled as 0 on existing rows; default alerts ON for bound chats once.
	var migrated string
	s.db.Raw("SELECT value FROM system_config WHERE key = ?", "telegram_notify_migrated").Scan(&migrated)
	if migrated != "1" {
		s.db.Exec("UPDATE telegram_configs SET notify_enabled = ? WHERE chat_id != 0", true)
		s.db.Exec(`
			INSERT INTO system_config (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			"telegram_notify_migrated", "1")
	}
	return nil
}

func (s *telegramConfigStore) Get() (*TelegramConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cfg TelegramConfig
	if err := s.db.First(&cfg, 1).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *telegramConfigStore) SaveToken(botToken string) error {
	return s.Save(botToken, "")
}

func (s *telegramConfigStore) Save(botToken, modelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg TelegramConfig
	result := s.db.First(&cfg, 1)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}
	cfg.ID = 1
	cfg.BotToken = botToken
	cfg.ModelID = modelID
	if cfg.ChatID == 0 && cfg.BindCode == "" {
		cfg.BindCode = newBindCode()
	}
	return s.db.Save(&cfg).Error
}

func newBindCode() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08X", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return strings.ToUpper(hex.EncodeToString(b))
}

func (s *telegramConfigStore) BindUser(chatID int64, username string, bindCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg TelegramConfig
	result := s.db.First(&cfg, 1)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}
	if cfg.ChatID != 0 {
		return fmt.Errorf("telegram already bound")
	}
	expected := strings.ToUpper(strings.TrimSpace(cfg.BindCode))
	got := strings.ToUpper(strings.TrimSpace(bindCode))
	if expected == "" || got == "" || expected != got {
		return fmt.Errorf("invalid bind code")
	}
	cfg.ID = 1
	cfg.ChatID = chatID
	cfg.Username = username
	cfg.BoundAt = time.Now()
	cfg.BindCode = ""
	if cfg.SelectedTraderID == "" {
		cfg.SelectedTraderID = "*"
	}
	return s.db.Save(&cfg).Error
}

func (s *telegramConfigStore) EnsureBindCode() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg TelegramConfig
	result := s.db.First(&cfg, 1)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return "", result.Error
	}
	if cfg.ChatID != 0 {
		return "", nil
	}
	if cfg.BindCode == "" {
		cfg.ID = 1
		cfg.BindCode = newBindCode()
		if err := s.db.Save(&cfg).Error; err != nil {
			return "", err
		}
	}
	return cfg.BindCode, nil
}

func (s *telegramConfigStore) IsBound() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cfg TelegramConfig
	if err := s.db.First(&cfg, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return cfg.ChatID != 0, nil
}

func (s *telegramConfigStore) GetBoundChatID() (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cfg TelegramConfig
	if err := s.db.First(&cfg, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return cfg.ChatID, nil
}

func (s *telegramConfigStore) Unbind() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Model(&TelegramConfig{}).Where("id = 1").Updates(map[string]interface{}{
		"chat_id":   0,
		"username":  "",
		"bind_code": newBindCode(),
	}).Error
}

func (s *telegramConfigStore) SetLanguage(lang string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg TelegramConfig
	result := s.db.First(&cfg, 1)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}
	cfg.ID = 1
	cfg.Language = lang
	return s.db.Save(&cfg).Error
}

func (s *telegramConfigStore) GetLanguage() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cfg TelegramConfig
	if err := s.db.First(&cfg, 1).Error; err != nil {
		return "en" // default: English
	}
	if cfg.Language == "" {
		return "en"
	}
	return cfg.Language
}

func (s *telegramConfigStore) SetNotifyEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg TelegramConfig
	result := s.db.First(&cfg, 1)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}
	cfg.ID = 1
	cfg.NotifyEnabled = enabled
	return s.db.Save(&cfg).Error
}

func (s *telegramConfigStore) IsNotifyEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cfg TelegramConfig
	if err := s.db.First(&cfg, 1).Error; err != nil {
		return true
	}
	return cfg.NotifyEnabled
}

func (s *telegramConfigStore) SetDigestEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg TelegramConfig
	result := s.db.First(&cfg, 1)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}
	cfg.ID = 1
	cfg.DigestEnabled = enabled
	return s.db.Save(&cfg).Error
}

func (s *telegramConfigStore) IsDigestEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cfg TelegramConfig
	if err := s.db.First(&cfg, 1).Error; err != nil {
		return true
	}
	return cfg.DigestEnabled
}

func (s *telegramConfigStore) GetPnlSwingThreshold() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cfg TelegramConfig
	if err := s.db.First(&cfg, 1).Error; err != nil {
		return 5.0
	}
	if cfg.PnlSwingThreshold <= 0 {
		return 5.0
	}
	return cfg.PnlSwingThreshold
}

func (s *telegramConfigStore) SetPnlSwingThreshold(threshold float64) error {
	if threshold <= 0 {
		threshold = 5.0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg TelegramConfig
	result := s.db.First(&cfg, 1)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}
	cfg.ID = 1
	cfg.PnlSwingThreshold = threshold
	return s.db.Save(&cfg).Error
}

// SelectedTraderAll is stored in selected_trader_id to mean "show every trader".
const SelectedTraderAll = "*"

func (s *telegramConfigStore) GetSelectedTraderID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cfg TelegramConfig
	if err := s.db.First(&cfg, 1).Error; err != nil {
		return SelectedTraderAll
	}
	if cfg.SelectedTraderID == "" {
		return SelectedTraderAll
	}
	return cfg.SelectedTraderID
}

func (s *telegramConfigStore) SetSelectedTraderID(traderID string) error {
	traderID = strings.TrimSpace(traderID)
	if traderID == "" || strings.EqualFold(traderID, "all") {
		traderID = SelectedTraderAll
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg TelegramConfig
	result := s.db.First(&cfg, 1)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}
	cfg.ID = 1
	cfg.SelectedTraderID = traderID
	return s.db.Save(&cfg).Error
}
