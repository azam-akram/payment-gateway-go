package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLevelFromEnv(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	assert.Equal(t, slog.LevelInfo, LevelFromEnv("LOG_LEVEL"), "unset should default to Info")

	t.Setenv("LOG_LEVEL", "not-a-level")
	assert.Equal(t, slog.LevelInfo, LevelFromEnv("LOG_LEVEL"), "invalid should default to Info")

	t.Setenv("LOG_LEVEL", "debug")
	assert.Equal(t, slog.LevelDebug, LevelFromEnv("LOG_LEVEL"))

	t.Setenv("LOG_LEVEL", "ERROR")
	assert.Equal(t, slog.LevelError, LevelFromEnv("LOG_LEVEL"))
}

func TestRequestIDHandler_StampsRequestIDFromContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(requestIDHandler{slog.NewJSONHandler(&buf, nil)})

	var capturedCtx context.Context
	router := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
	}))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	require.NotNil(t, capturedCtx)

	logger.InfoContext(capturedCtx, "test message")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "test message", entry["msg"])
	assert.Equal(t, middleware.GetReqID(capturedCtx), entry["request_id"])
	assert.NotEmpty(t, entry["request_id"])
}

func TestRequestIDHandler_NoRequestIDOnBackgroundContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(requestIDHandler{slog.NewJSONHandler(&buf, nil)})

	logger.InfoContext(context.Background(), "test message")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.NotContains(t, entry, "request_id")
}
