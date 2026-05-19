package user_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/romangolovachev/selfcare/internal/auth"
	"github.com/romangolovachev/selfcare/internal/user"
	"github.com/romangolovachev/selfcare/pkg/jwtutil"
	"github.com/romangolovachev/selfcare/pkg/testutil"
)

func setupUserServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	pool := testutil.StartPostgres(t)
	jwtSvc := jwtutil.New("test-secret-32-chars-long-padding")

	authRepo := auth.NewRepository(pool)
	authSvc := auth.NewService(authRepo, jwtSvc)
	result, err := authSvc.Register(t.Context(), auth.RegisterInput{
		Email:    "testuser@example.com",
		Password: "strongpassword",
		Name:     "TestUser",
	})
	require.NoError(t, err)

	userRepo := user.NewRepository(pool)
	userSvc := user.NewService(userRepo)
	h := user.NewHandler(userSvc)

	srv := httptest.NewServer(jwtSvc.MiddlewareWithVersionCheck(authSvc.VersionChecker())(h.Routes()))
	t.Cleanup(srv.Close)
	return srv, result.Token
}

func authDo(t *testing.T, method, url, token string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestGetMe_Success(t *testing.T) {
	srv, token := setupUserServer(t)

	resp := authDo(t, http.MethodGet, srv.URL+"/me", token, nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "testuser@example.com", result["email"])
	assert.Equal(t, "TestUser", result["name"])
}

func TestGetMe_Unauthorized(t *testing.T) {
	srv, _ := setupUserServer(t)

	resp := authDo(t, http.MethodGet, srv.URL+"/me", "", nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestUpdateMe_Success(t *testing.T) {
	srv, token := setupUserServer(t)

	body, _ := json.Marshal(map[string]string{"name": "UpdatedName", "avatar_url": ""})
	resp := authDo(t, http.MethodPut, srv.URL+"/me", token, body)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Verify change
	resp2 := authDo(t, http.MethodGet, srv.URL+"/me", token, nil)
	defer resp2.Body.Close()
	var result map[string]any
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&result))
	assert.Equal(t, "UpdatedName", result["name"])
}

func TestGetStats_Success(t *testing.T) {
	srv, token := setupUserServer(t)

	resp := authDo(t, http.MethodGet, srv.URL+"/me/stats", token, nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotNil(t, result["streak_days"])
	assert.NotNil(t, result["total_entries"])
}
