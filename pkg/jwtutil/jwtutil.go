package jwtutil

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Service struct {
	secret []byte
}

type Claims struct {
	UserID       int64 `json:"user_id"`
	TokenVersion int   `json:"tv"`
	jwt.RegisteredClaims
}

type ctxKey struct{}

func New(secret string) *Service {
	return &Service{secret: []byte(secret)}
}

func (s *Service) Sign(userID int64, tokenVersion int) (string, error) {
	claims := Claims{
		UserID:       userID,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

func (s *Service) Parse(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// VersionChecker validates that the token version in the JWT matches the current
// version stored in the database. Return false to reject the token.
type VersionChecker func(ctx context.Context, userID int64, tokenVersion int) bool

func (s *Service) Middleware(next http.Handler) http.Handler {
	return s.MiddlewareWithVersionCheck(nil)(next)
}

// MiddlewareWithVersionCheck returns a middleware that additionally validates
// the token version via the provided checker (pass nil to skip version check).
func (s *Service) MiddlewareWithVersionCheck(check VersionChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			claims, err := s.Parse(strings.TrimPrefix(header, "Bearer "))
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if check != nil && !check(r.Context(), claims.UserID, claims.TokenVersion) {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, claims)))
		})
	}
}

func UserID(ctx context.Context) int64 {
	c, _ := ctx.Value(ctxKey{}).(*Claims)
	if c == nil {
		return 0
	}
	return c.UserID
}
