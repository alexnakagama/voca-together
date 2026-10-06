package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	New(slog.New(slog.DiscardHandler), nil, nil, nil, nil, Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if body := rec.Body.String(); body != `{"status":"ok"}`+"\n" {
		t.Errorf("body = %q", body)
	}
}

func TestHealthzRejectsOtherMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	New(slog.New(slog.DiscardHandler), nil, nil, nil, nil, Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
