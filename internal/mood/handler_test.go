package mood_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/romangolovachev/selfcare/internal/auth"
	"github.com/romangolovachev/selfcare/internal/mood"
	"github.com/romangolovachev/selfcare/internal/notification"
	"github.com/romangolovachev/selfcare/pkg/jwtutil"
	"github.com/romangolovachev/selfcare/pkg/testutil"
)

func setupMoodServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	pool := testutil.StartPostgres(t)
	jwtSvc := jwtutil.New("test-secret-32-chars-long-padding")

	// Register a user and get a token for authenticated requests
	authRepo := auth.NewRepository(pool)
	authSvc := auth.NewService(authRepo, jwtSvc)
	result, err := authSvc.Register(t.Context(), auth.RegisterInput{
		Email:    "mooduser@example.com",
		Password: "strongpassword",
		Name:     "MoodUser",
	})
	require.NoError(t, err)

	notifRepo := notification.NewRepository(pool)
	notifSvc := notification.NewService(pool, notifRepo)
	moodRepo := mood.NewRepository(pool)
	moodSvc := mood.NewService(moodRepo, pool, notifSvc)
	h := mood.NewHandler(moodSvc)

	srv := httptest.NewServer(jwtSvc.MiddlewareWithVersionCheck(authSvc.VersionChecker())(h.Routes()))
	t.Cleanup(srv.Close)
	return srv, result.Token
}

func authRequest(t *testing.T, method, url, token string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestCreateMood_Success(t *testing.T) {
	srv, token := setupMoodServer(t)

	body, _ := json.Marshal(map[string]any{
		"score": 7, "stress_level": 3, "work_hours": 8.0, "note": "good day", "tags": []string{},
	})
	resp := authRequest(t, http.MethodPost, srv.URL+"/", token, body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	var entry map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&entry))
	assert.Equal(t, float64(7), entry["score"])
}

func TestCreateMood_InvalidScore(t *testing.T) {
	srv, token := setupMoodServer(t)

	body, _ := json.Marshal(map[string]any{"score": 0, "stress_level": 0, "work_hours": 0.0})
	resp := authRequest(t, http.MethodPost, srv.URL+"/", token, body)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestGetTodayMood_NotFound(t *testing.T) {
	srv, token := setupMoodServer(t)

	resp := authRequest(t, http.MethodGet, srv.URL+"/today", token, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetTodayMood_Found(t *testing.T) {
	srv, token := setupMoodServer(t)

	// Create a mood entry first
	body, _ := json.Marshal(map[string]any{
		"score": 8, "stress_level": 2, "work_hours": 7.5, "tags": []string{},
	})
	createResp := authRequest(t, http.MethodPost, srv.URL+"/", token, body)
	createResp.Body.Close()

	resp := authRequest(t, http.MethodGet, srv.URL+"/today", token, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var entry map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&entry))
	assert.Equal(t, float64(8), entry["score"])
}

func TestListMoods_Empty(t *testing.T) {
	srv, token := setupMoodServer(t)

	resp := authRequest(t, http.MethodGet, fmt.Sprintf("%s/?page=1", srv.URL), token, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCreateMood_Unauthenticated(t *testing.T) {
	srv, _ := setupMoodServer(t)

	body, _ := json.Marshal(map[string]any{"score": 7, "tags": []string{}})
	resp, err := http.Post(srv.URL+"/", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
