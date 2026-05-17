// Package subscription manages the trial window.
package subscription

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const TrialDays = 5

var ErrTrialExpiredAndLimitReached = errors.New("trial expired: upgrade to premium")

// Status describes the user's current subscription state.
type Status struct {
	InTrial      bool
	TrialDayNum  int // 1–5 while in trial, 0 = not in trial
	IsPremium    bool
	TrialStarted bool // true if trial_started_at is set (even if expired)
}

// Service handles trial tracking.
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
	err := s.db.QueryRow(ctx,
		`SELECT is_premium, trial_started_at FROM users WHERE id = $1`,
		userID,
	).Scan(&isPremium, &trialStarted)
	if err != nil {
		return nil, err
	}

	st := &Status{IsPremium: isPremium, TrialStarted: trialStarted != nil}
	if trialStarted != nil {
		days := int(time.Since(*trialStarted).Hours()/24) + 1
		if days <= TrialDays {
			st.InTrial = true
			st.TrialDayNum = days
		}
	}
	return st, nil
}

// CheckAndConsumeLR verifies access for a Live Response request.
// Starts the trial on first use. Returns ErrTrialExpiredAndLimitReached
// when the user has neither an active trial nor a premium subscription.
func (s *Service) CheckAndConsumeLR(ctx context.Context, userID int64) (*Status, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var isPremium bool
	var trialStarted *time.Time
	err = tx.QueryRow(ctx,
		`SELECT is_premium, trial_started_at FROM users WHERE id = $1 FOR UPDATE`,
		userID,
	).Scan(&isPremium, &trialStarted)
	if err != nil {
		return nil, err
	}

	// Start trial on very first LR use.
	if trialStarted == nil {
		if _, err := tx.Exec(ctx,
			`UPDATE users SET trial_started_at = NOW() WHERE id = $1 AND trial_started_at IS NULL`,
			userID,
		); err != nil {
			return nil, err
		}
		now := time.Now()
		trialStarted = &now
	}

	st := &Status{IsPremium: isPremium, TrialStarted: trialStarted != nil}
	if trialStarted != nil {
		days := int(time.Since(*trialStarted).Hours()/24) + 1
		if days <= TrialDays {
			st.InTrial = true
			st.TrialDayNum = days
		}
	}

	if isPremium || st.InTrial {
		return st, tx.Commit(ctx)
	}

	return st, ErrTrialExpiredAndLimitReached
}
