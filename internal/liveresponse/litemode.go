package liveresponse

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// IsLiteMode returns true if the user should receive simplified Live Response.
//
// Triggers (either is sufficient):
//  1. 2+ heavy events (weight ≥ 3) already logged today.
//  2. 3 consecutive days with stress > user's historical average × 1.3.
func IsLiteMode(ctx context.Context, db *pgxpool.Pool, userID int64) (bool, error) {
	// Trigger 1: heavy sessions today
	var heavyToday int
	err := db.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM live_response_sessions
		 WHERE user_id = $1
		   AND DATE(created_at AT TIME ZONE 'UTC') = CURRENT_DATE
		   AND event_weight >= 3`,
		userID,
	).Scan(&heavyToday)
	if err != nil {
		return false, err
	}
	if heavyToday >= 2 {
		return true, nil
	}

	// Trigger 2: 3 consecutive recent days with stress > avg×1.3
	// We count distinct days in the last 3 calendar days that have high stress.
	// If all 3 are above threshold → lite mode.
	var highStressDays int
	err = db.QueryRow(ctx,
		`WITH threshold AS (
		   SELECT COALESCE(AVG(stress_level), 5) * 1.3 AS val
		   FROM mood_entries
		   WHERE user_id = $1
		 )
		 SELECT COUNT(DISTINCT DATE(m.created_at AT TIME ZONE 'UTC'))
		 FROM mood_entries m, threshold t
		 WHERE m.user_id = $1
		   AND DATE(m.created_at AT TIME ZONE 'UTC') >= CURRENT_DATE - INTERVAL '2 days'
		   AND m.stress_level > t.val`,
		userID,
	).Scan(&highStressDays)
	if err != nil {
		return false, err
	}
	return highStressDays >= 3, nil
}
