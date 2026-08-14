package store

import (
	"errors"
	"fmt"
	"nofx/auth"
	"nofx/logger"
	"os"
	"strings"

	"gorm.io/gorm"
)

// unusablePasswordHash is a valid bcrypt hash used when no RESET_LOGIN_PASSWORD
// is configured. The owner should sign in via Telegram /weblogin instead.
const unusablePasswordHash = "$2a$10$0iF0bCoQLJ6Ph1bF.MXwHOW.IMTxQjeEW.w38dctRQAB2kwB6ga1q"

// RepairOwnerAccount recreates the configured owner user row and reassigns
// AI models, exchanges, and strategies referenced by owner traders when the
// user row was deleted but trader data still references TELEGRAM_OWNER_USER_ID.
func (s *Store) RepairOwnerAccount() error {
	ownerID := strings.TrimSpace(os.Getenv("TELEGRAM_OWNER_USER_ID"))
	if ownerID == "" {
		return nil
	}

	traders, err := s.Trader().List(ownerID)
	if err != nil {
		return fmt.Errorf("list owner traders: %w", err)
	}
	if len(traders) == 0 {
		return nil
	}

	repaired := false

	_, userErr := s.User().GetByID(ownerID)
	if userErr != nil {
		if !errors.Is(userErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("lookup owner user: %w", userErr)
		}
		email, err := ownerLoginEmail(s, ownerID)
		if err != nil {
			return err
		}
		hash, err := ownerLoginPasswordHash()
		if err != nil {
			return err
		}
		if err := s.User().Create(&User{
			ID:           ownerID,
			Email:        email,
			PasswordHash: hash,
		}); err != nil {
			return fmt.Errorf("create owner user: %w", err)
		}
		logger.Infof("✓ Restored owner user %s (%s) with %d traders", ownerID, email, len(traders))
		repaired = true
	}

	for _, tr := range traders {
		if tr == nil {
			continue
		}
		if tr.AIModelID != "" {
			model, err := s.AIModel().GetByID(tr.AIModelID)
			if err == nil && model.UserID != ownerID {
				if err := s.AIModel().AdoptModel(model.ID, ownerID); err != nil {
					logger.Warnf("Failed to reassign AI model %s to owner %s: %v", model.ID, ownerID, err)
				} else {
					logger.Infof("✓ Reassigned AI model %s to owner %s", model.ID, ownerID)
					repaired = true
				}
			}
		}
		if tr.ExchangeID != "" {
			exchange, err := s.Exchange().GetByIDAny(tr.ExchangeID)
			if err == nil && exchange.UserID != ownerID {
				if err := s.Exchange().AdoptExchange(exchange.ID, ownerID); err != nil {
					logger.Warnf("Failed to reassign exchange %s to owner %s: %v", exchange.ID, ownerID, err)
				} else {
					logger.Infof("✓ Reassigned exchange %s to owner %s", exchange.ID, ownerID)
					repaired = true
				}
			}
		}
		if tr.StrategyID != "" {
			strategy, err := s.Strategy().GetByIDAny(tr.StrategyID)
			if err == nil && strategy.UserID != ownerID {
				if err := s.Strategy().AdoptStrategy(strategy.ID, ownerID); err != nil {
					logger.Warnf("Failed to reassign strategy %s to owner %s: %v", strategy.ID, ownerID, err)
				} else {
					logger.Infof("✓ Reassigned strategy %s to owner %s", strategy.ID, ownerID)
					repaired = true
				}
			}
		}
	}

	if repaired {
		logger.Infof("✅ Owner account repair complete for %s", ownerID)
	}
	return nil
}

func ownerLoginEmail(s *Store, ownerID string) (string, error) {
	if email := strings.TrimSpace(os.Getenv("OWNER_LOGIN_EMAIL")); email != "" {
		if existing, err := s.User().GetByEmail(email); err == nil && existing.ID != ownerID {
			return "", fmt.Errorf("OWNER_LOGIN_EMAIL %q already belongs to user %s", email, existing.ID)
		}
		return email, nil
	}

	candidate := fmt.Sprintf("owner-%s@nofx.local", ownerID[:8])
	if existing, err := s.User().GetByEmail(candidate); err == nil && existing.ID != ownerID {
		candidate = fmt.Sprintf("owner-%s@nofx.local", strings.ReplaceAll(ownerID, "-", "")[:12])
	}
	return candidate, nil
}

func ownerLoginPasswordHash() (string, error) {
	if pw := strings.TrimSpace(os.Getenv("RESET_LOGIN_PASSWORD")); pw != "" {
		return auth.HashPassword(pw)
	}
	return unusablePasswordHash, nil
}
