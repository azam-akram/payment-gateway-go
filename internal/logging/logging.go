// Package logging configures the application's structured logger.
//
// Every log line goes out as JSON on stdout via log/slog, and any line
// logged while handling an HTTP request is automatically stamped with the
// chi request ID for that request - so the request-received line, the
// payment-processed line, and any bank retry warnings for a single request
// can be grep'd out together by request_id, without every call site having
// to thread a logger or look the ID up itself.
package logging

import (
	"context"
	"log/slog"
	"os"

	"github.com/go-chi/chi/v5/middleware"
)

// New builds a JSON slog.Logger writing to os.Stdout at the given level.
func New(level slog.Level) *slog.Logger {
	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(requestIDHandler{base})
}

// LevelFromEnv parses key as an slog.Level (DEBUG/INFO/WARN/ERROR, case
// insensitive), defaulting to Info if unset or invalid.
func LevelFromEnv(key string) slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(os.Getenv(key))); err != nil {
		return slog.LevelInfo
	}
	return level
}

// requestIDHandler wraps a slog.Handler and adds the chi request ID (if
// any is set on the context) to every record as request_id.
type requestIDHandler struct {
	slog.Handler
}

func (h requestIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := middleware.GetReqID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h requestIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return requestIDHandler{h.Handler.WithAttrs(attrs)}
}

func (h requestIDHandler) WithGroup(name string) slog.Handler {
	return requestIDHandler{h.Handler.WithGroup(name)}
}
