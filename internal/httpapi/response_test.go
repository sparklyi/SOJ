package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type testModule struct{}

func (testModule) RegisterRoutes(group *gin.RouterGroup) {
	group.GET("/module-ping", func(c *gin.Context) {
		OK(c, gin.H{"pong": true})
	})
}

type panicModule struct{}

func (panicModule) RegisterRoutes(group *gin.RouterGroup) {
	group.GET("/panic", func(c *gin.Context) {
		panic("boom")
	})
}

func TestResponseEnvelopeIncludesRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(RouterOptions{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set(HeaderRequestID, "req-test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var body Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.RequestID != "req-test" {
		t.Fatalf("request_id = %q, want req-test", body.RequestID)
	}
	if body.Error != nil {
		t.Fatalf("error = %+v, want nil", body.Error)
	}
}

func TestNoContentWritesEmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID())
	router.DELETE("/resource", func(c *gin.Context) {
		NoContent(c)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/resource", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
}

func TestReadyzFailureUsesErrorEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(RouterOptions{
		ReadyCheck: func(context.Context) error {
			return errors.New("redis unavailable")
		},
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 body=%s", rec.Code, rec.Body.String())
	}

	var body Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error == nil || body.Error.Code != "service_unavailable" {
		t.Fatalf("error = %+v, want service_unavailable", body.Error)
	}
}

func TestNewRouterRegistersModules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(RouterOptions{
		Modules: []Module{testModule{}},
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/module-ping", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
}

func TestNewRouterRecordsHTTPMetricsAndExposesMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics := &recordingHTTPMetrics{}
	router := NewRouter(RouterOptions{Metrics: metrics})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if len(metrics.requests) != 1 {
		t.Fatalf("recorded requests = %d, want 1", len(metrics.requests))
	}
	got := metrics.requests[0]
	if got.method != http.MethodGet || got.route != "/healthz" || got.status != http.StatusOK || got.duration <= 0 {
		t.Fatalf("recorded metric = %+v", got)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "metrics ok" {
		t.Fatalf("metrics body = %q, want metrics ok", rec.Body.String())
	}
}

func TestHTTPMetricsRecordsRecoveredPanics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics := &recordingHTTPMetrics{}
	router := NewRouter(RouterOptions{
		Metrics: metrics,
		Modules: []Module{panicModule{}},
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if len(metrics.requests) != 1 {
		t.Fatalf("recorded requests = %d, want 1", len(metrics.requests))
	}
	got := metrics.requests[0]
	if got.method != http.MethodGet || got.route != "/api/v1/panic" || got.status != http.StatusInternalServerError {
		t.Fatalf("recorded metric = %+v", got)
	}
}

type recordedHTTPRequest struct {
	method   string
	route    string
	status   int
	duration time.Duration
}

type recordingHTTPMetrics struct {
	requests []recordedHTTPRequest
}

func (m *recordingHTTPMetrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("metrics ok"))
	})
}

func (m *recordingHTTPMetrics) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	m.requests = append(m.requests, recordedHTTPRequest{method: method, route: route, status: status, duration: duration})
}
