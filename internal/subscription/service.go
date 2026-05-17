// Package subscription manages the trial window and free-tier LR counter.
package subscription

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	TrialDays      = 5
	FreeLRPerMonth = 3
)

var ErrTrialExpiredAndLimitReached = errors.New("trial expired and free limit reached")

// Status describes the user's current subscription state.
type Status struct {
	InTrial       bool
	TrialDayNum   int // 1–5 while in trial, 0 = expired
	IsPremium     bool
	FreeUsedMonth int // uses THIS calendar month
}

// Service handles trial tracking and usage metering.
type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// GetStatus returns the current subscription status for a user.
// FreeUsedMonth reflects only the current calendar month.
func (s *Service) GetStatus(ctx context.Context, userID int64) (*Status, error) {
	var isPremium bool
	var trialStarted *time.Time
	var usedMonth int
	var storedKey string
	err := s.db.QueryRow(ctx,
		`SELECT is_premium, trial_started_at, lr_used_month, lr_month_key
		 FROM users WHERE id = $1`,
		userID,
	).Scan(&isPremium, &trialStarted, &usedMonth, &storedKey)
	if err != nil {
		return nil, err
	}

	st := &Status{IsPremium: isPremium}

	// Only report the counter if it belongs to the current month.
	if storedKey == monthKey() {
		st.FreeUsedMonth = usedMonth
	}

	if trialStarted != nil {
		days := int(time.Since(*trialStarted).Hours()/24) + 1
		if days <= TrialDays {
			st.InTrial = true
			st.TrialDayNum = days
		}
	}
	return st, nil
}

// CheckAndConsumeLR verifies access and increments the free-tier counter atomically.
// Returns ErrTrialExpiredAndLimitReached when the user should see the paywall.
//
// Access order:
//  1. Premium → always allowed, no counter consumed.
//  2. In trial (days 1–5) → always allowed, no counter consumed.
//  3. Free tier with remaining monthly quota → allowed, counter incremented.
//  4. Free tier quota exhausted → ErrTrialExpiredAndLimitReached.
func (s *Service) CheckAndConsumeLR(ctx context.Context, userID int64) (*Status, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Lock the row for the duration of this transaction to prevent race on counter.
	var isPremium bool
	var trialStarted *time.Time
	var usedMonth int
	var storedKey string
	err = tx.QueryRow(ctx,
		`SELECT is_premium, trial_started_at, lr_used_month, lr_month_key
		 FROM users WHERE id = $1 FOR UPDATE`,
		userID,
	).Scan(&isPremium, &trialStarted, &usedMonth, &storedKey)
	if err != nil {
		return nil, err
	}

	// Start trial on very first LR use.
	if trialStarted == nil {
		if _, err := tx.Exec(ctx,
			`UPDATE users SET trial_started_at = NOW()
			 WHERE id = $1 AND trial_started_at IS NULL`,
			userID,
		); err != nil {
			return nil, err
		}
		now := time.Now()
		trialStarted = &now
	}

	st := &Status{IsPremium: isPremium}
	if trialStarted != nil {
		days := int(time.Since(*trialStarted).Hours()/24) + 1
		if days <= TrialDays {
			st.InTrial = true
			st.TrialDayNum = days
		}
	}

	// Premium or in trial: always allow.
	if isPremium || st.InTrial {
		return st, tx.Commit(ctx)
	}

	// Free tier: effective count is 0 if the stored key is for a past month.
	curKey := monthKey()
	effectiveUsed := 0
	if storedKey == curKey {
		effectiveUsed = usedMonth
	}

	if effectiveUsed >= FreeLRPerMonth {
		// No commit needed — read-only path inside the transaction.
		return st, ErrTrialExpiredAndLimitReached
	}

	// Increment atomically (CASE handles month rollover).
	if _, err := tx.Exec(ctx,
		`UPDATE users
		 SET lr_used_month = CASE WHEN lr_month_key = $2 THEN lr_used_month + 1 ELSE 1 END,
		     lr_month_key  = $2
		 WHERE id = $1`,
		userID, curKey,
	); err != nil {
		return nil, err
	}

	st.FreeUsedMonth = effectiveUsed + 1
	return st, tx.Commit(ctx)
}

func (s *Service) startTrial(ctx context.Context, userID int64) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET trial_started_at = NOW()
		 WHERE id = $1 AND trial_started_at IS NULL`,
		userID,
	)
	return err
}

func monthKey() string {
	t := time.Now()
	return fmt.Sprintf("%d-%02d", t.Year(), t.Month())
}
