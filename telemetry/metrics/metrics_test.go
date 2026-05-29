package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerServesMetrics(t *testing.T) {
	handler := Handler()
	if handler == nil {
		t.Fatal("Handler() returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("metrics handler status = %d, want %d", rec.Code, http.StatusOK)
	}
}
