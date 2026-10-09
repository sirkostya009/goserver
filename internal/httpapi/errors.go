package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"goserver/domain"
	"goserver/internal/telemetry"
)

// handleError is the central onerror: domain sentinels map to statuses,
// anything else is a 500, logged at Error, which makes it a Sentry issue.
func handleError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.Canceled) && ctx.Err() != nil:
		// the client left, which is no fault of ours and nobody reads the reply
		slog.InfoContext(ctx, "handler", slog.Any("err", err))
		http.Error(w, "client closed request", statusClientClosed)
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, domain.ErrUnknownTenant):
		// also what a registered tenant looks like before cmd/migrate has run for it
		slog.WarnContext(ctx, "handler", slog.Any("err", err))
		http.Error(w, domain.ErrUnknownTenant.Error(), http.StatusNotFound)
	case errors.Is(err, domain.ErrConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, domain.ErrMalformed):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, domain.ErrInvalid):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, domain.ErrUnauthenticated):
		unauthenticated(w, err)
	case errors.Is(err, domain.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	default:
		slog.ErrorContext(ctx, "handler", slog.Any("err", err))
		telemetry.FailSpan(ctx, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// statusClientClosed is nginx's, net/http has none for a client that hung up
const statusClientClosed = 499

// err is nil when the request is malformed with no decode error attached
func badRequest(w http.ResponseWriter, err error) {
	msg := "bad request"
	if err != nil {
		msg = err.Error()
	}
	http.Error(w, msg, http.StatusBadRequest)
}

func notFound(w http.ResponseWriter) {
	http.Error(w, "not found", http.StatusNotFound)
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
