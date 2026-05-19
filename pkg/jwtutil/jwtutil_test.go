package jwtutil_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/romangolovachev/selfcare/pkg/jwtutil"
)

func TestSignParse_RoundTrip(t *testing.T) {
	svc := jwtutil.New("super-secret-key-32-chars-long!!")

	token, err := svc.Sign(42, 3)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := svc.Parse(token)
	require.NoError(t, err)
	assert.Equal(t, int64(42), claims.UserID)
	assert.Equal(t, 3, claims.TokenVersion)
}

func TestParse_WrongSecret(t *testing.T) {
	svc1 := jwtutil.New("secret-one-32-chars-long-padding!")
	svc2 := jwtutil.New("secret-two-32-chars-long-padding!")

	token, err := svc1.Sign(1, 1)
	require.NoError(t, err)

	_, err = svc2.Parse(token)
	assert.Error(t, err)
}

func TestParse_ExpiredToken(t *testing.T) {
	svc := jwtutil.New("super-secret-key-32-chars-long!!")

	// Manually craft an already-expired token
	claims := jwtutil.Claims{
		UserID:       1,
		TokenVersion: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("super-secret-key-32-chars-long!!"))
	require.NoError(t, err)

	_, err = svc.Parse(raw)
	assert.Error(t, err)
}

func TestParse_MalformedToken(t *testing.T) {
	svc := jwtutil.New("super-secret-key-32-chars-long!!")
	_, err := svc.Parse("not.a.jwt")
	assert.Error(t, err)
}
