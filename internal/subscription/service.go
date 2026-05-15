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
	TrialDays    = 5
	FreeLRPerMonth = 3
)

var ErrTrialExpiredAndLimitReached = errors.New("trial expired and free limit reached")

// Status describes the user's current subscription state.
type Status struct {
	InTrial       bool
	TrialDayNum   int  // 1–5 while in trial, 0 = expired
	IsPremium     bool
	FreeUsedMonth int
}

// Service handles trial tracking and usage metering.
type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// GetStatus returns the current subscription status for a user.
func (s *Service) GetStatus(ctx context.Context, userID int64) (*Status, error) {
	var isPremium bool
	var trialStarted *time.Time
	var usedMonth int
	err := s.db.QueryRow(ctx,
		`SELECT is_premium, trial_started_at, lr_used_month FROM users WHERE id = $1`,
		userID,
	).Scan(&isPremium, &trialStarted, &usedMonth)
	if err != nil {
		return nil, err
	}

	st := &Status{IsPremium: isPremium, FreeUsedMonth: usedMonth}
	if trialStarted != nil {
		days := int(time.Since(*trialStarted).Hours()/24) + 1
		if days <= TrialDays {
			st.InTrial = true
			st.TrialDayNum = days
		}
	}
	return st, nil
}

// CheckAndConsumeLR verifies access and, if on free tier, increments the counter.
// Returns ErrTrialExpiredAndLimitReached when the user should see the paywall.
func (s *Service) CheckAndConsumeLR(ctx context.Context, userID int64) (*Status, error) {
	st, err := s.GetStatus(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Premium and trial always pass without consuming the counter
	if st.IsPremium || st.InTrial {
		// Start trial on first use if not started yet
		if !st.InTrial && !st.IsPremium {
			if err := s.startTrial(ctx, userID); err != nil {
				return nil, err
			}
			st.InTrial = true
			st.TrialDayNum = 1
		}
		return st, nil
	}

	// Free tier: check monthly counter (reset on new month automatically)
	currentMonthKey := monthKey()
	if st.FreeUsedMonth >= FreeLRPerMonth {
		return st, ErrTrialExpiredAndLimitReached
	}

	// Increment (with automatic month reset)
	_, err = s.db.Exec(ctx,
		`UPDATE users
		 SET lr_used_month = CASE WHEN lr_month_key = $2 THEN lr_used_month + 1 ELSE 1 END,
		     lr_month_key  = $2
		 WHERE id = $1`,
		userID, currentMonthKey,
	)
	if err != nil {
		return nil, err
	}
	st.FreeUsedMonth++
	return st, nil
}

// StartTrialIfNeeded sets trial_started_at to NOW() on the user's first LR use.
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
