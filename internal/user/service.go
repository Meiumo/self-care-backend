package user

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

type Profile struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	IsPremium bool   `json:"is_premium"`
}

func (s *Service) Me(ctx context.Context, userID int64) (*Profile, error) {
	u, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &Profile{ID: u.ID, Email: u.Email, Name: u.Name, AvatarURL: u.AvatarURL, IsPremium: u.IsPremium}, nil
}

func (s *Service) UpdateProfile(ctx context.Context, userID int64, name, avatarURL string) error {
	return s.repo.UpdateProfile(ctx, userID, name, avatarURL)
}

func (s *Service) SetPremium(ctx context.Context, userID int64, premium bool) error {
	return s.repo.SetPremium(ctx, userID, premium)
}

var ErrNotFound = pgx.ErrNoRows
