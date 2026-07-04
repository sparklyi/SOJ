package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type testModule struct{}

func (testModule) RegisterRoutes(group *gin.RouterGroup) {
	group.GET("/module-ping", func(c *gin.Context) {
		OK(c, gin.H{"pong": true})
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
