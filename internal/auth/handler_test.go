package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/romangolovachev/selfcare/internal/auth"
	"github.com/romangolovachev/selfcare/pkg/jwtutil"
	"github.com/romangolovachev/selfcare/pkg/testutil"
)

func setupAuthServer(t *testing.T) *httptest.Server {
	t.Helper()
	pool := testutil.StartPostgres(t)
	jwtSvc := jwtutil.New("test-secret-32-chars-long-padding")
	repo := auth.NewRepository(pool)
	svc := auth.NewService(repo, jwtSvc)
	h := auth.NewHandler(svc)
	srv := httptest.NewServer(h.Routes(jwtSvc))
	t.Cleanup(srv.Close)
	return srv
}

func TestRegisterHandler_Success(t *testing.T) {
	srv := setupAuthServer(t)

	body, _ := json.Marshal(map[string]string{
		"email": "alice@example.com", "password": "strongpassword", "name": "Alice",
	})
	resp, err := http.Post(srv.URL+"/register", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["token"])
	assert.NotZero(t, result["user_id"])
}

func TestRegisterHandler_DuplicateEmail(t *testing.T) {
	srv := setupAuthServer(t)

	body, _ := json.Marshal(map[string]string{
		"email": "dup@example.com", "password": "strongpassword", "name": "Dup",
	})
	// First registration
	resp, _ := http.Post(srv.URL+"/register", "application/json", bytes.NewReader(body))
	resp.Body.Close()
	// Second registration with the same email
	resp, err := http.Post(srv.URL+"/register", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestRegisterHandler_WeakPassword(t *testing.T) {
	srv := setupAuthServer(t)

	body, _ := json.Marshal(map[string]string{
		"email": "user@example.com", "password": "short", "name": "User",
	})
	resp, err := http.Post(srv.URL+"/register", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestLoginHandler_Success(t *testing.T) {
	srv := setupAuthServer(t)

	// Register first
	reg, _ := json.Marshal(map[string]string{
		"email": "bob@example.com", "password": "strongpassword", "name": "Bob",
	})
	regResp, _ := http.Post(srv.URL+"/register", "application/json", bytes.NewReader(reg))
	regResp.Body.Close()

	login, _ := json.Marshal(map[string]string{
		"email": "bob@example.com", "password": "strongpassword",
	})
	resp, err := http.Post(srv.URL+"/login", "application/json", bytes.NewReader(login))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["token"])
}

func TestLoginHandler_WrongPassword(t *testing.T) {
	srv := setupAuthServer(t)

	reg, _ := json.Marshal(map[string]string{
		"email": "carol@example.com", "password": "strongpassword", "name": "Carol",
	})
	regResp, _ := http.Post(srv.URL+"/register", "application/json", bytes.NewReader(reg))
	regResp.Body.Close()

	login, _ := json.Marshal(map[string]string{
		"email": "carol@example.com", "password": "wrongpassword",
	})
	resp, err := http.Post(srv.URL+"/login", "application/json", bytes.NewReader(login))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
