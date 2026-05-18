package liveresponse

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EventType is a row from the event_types table.
type EventType struct {
	Name          string
	Emoji         string
	Response      ResponseType
	Weight        int
	FollowupHours int
	Chips         []string
	Opener        string
	SortOrder     int
}

// EventTypeRepo loads event types from the database.
type EventTypeRepo struct {
	db *pgxpool.Pool
}

func NewEventTypeRepo(db *pgxpool.Pool) *EventTypeRepo {
	return &EventTypeRepo{db: db}
}

func (r *EventTypeRepo) All(ctx context.Context) ([]*EventType, error) {
	rows, err := r.db.Query(ctx,
		`SELECT name, emoji, response_type, weight, followup_hours, chips, opener, sort_order
		 FROM event_types ORDER BY sort_order, name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*EventType
	for rows.Next() {
		et := &EventType{}
		var rt string
		if err := rows.Scan(&et.Name, &et.Emoji, &rt, &et.Weight, &et.FollowupHours, &et.Chips, &et.Opener, &et.SortOrder); err != nil {
			return nil, err
		}
		et.Response = parseResponseType(rt)
		out = append(out, et)
	}
	return out, rows.Err()
}

func (r *EventTypeRepo) FindByName(ctx context.Context, name string) (*EventType, error) {
	et := &EventType{}
	var rt string
	err := r.db.QueryRow(ctx,
		`SELECT name, emoji, response_type, weight, followup_hours, chips, opener, sort_order
		 FROM event_types WHERE name = $1`,
		name,
	).Scan(&et.Name, &et.Emoji, &rt, &et.Weight, &et.FollowupHours, &et.Chips, &et.Opener, &et.SortOrder)
	if err != nil {
		return nil, fmt.Errorf("event type %q not found: %w", name, err)
	}
	et.Response = parseResponseType(rt)
	return et, nil
}

func parseResponseType(s string) ResponseType {
	switch s {
	case "instant":
		return ResponseInstant
	case "advice":
		return ResponseAdvice
	default:
		return ResponseNothing
	}
}
