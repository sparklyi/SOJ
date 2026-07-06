package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	HeaderRequestID  = "X-Request-ID"
	ContextRequestID = "request_id"
)

type MiddlewareSet struct {
	CORS         gin.HandlerFunc
	RateLimit    gin.HandlerFunc
	RequestID    gin.HandlerFunc
	Auth         gin.HandlerFunc
	RequireAdmin gin.HandlerFunc
	RequireRoot  gin.HandlerFunc
}

func DefaultMiddlewareSet() MiddlewareSet {
	return MiddlewareSet{
		CORS:         noopMiddleware(),
		RateLimit:    noopMiddleware(),
		RequestID:    RequestID(),
		Auth:         noopMiddleware(),
		RequireAdmin: noopMiddleware(),
		RequireRoot:  noopMiddleware(),
	}
}

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(HeaderRequestID)
		if requestID == "" {
			requestID = newRequestID()
		}
		c.Set(ContextRequestID, requestID)
		c.Writer.Header().Set(HeaderRequestID, requestID)
		c.Next()
	}
}

func RecordHTTPMetrics(metrics HTTPMetrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		metrics.ObserveHTTPRequest(c.Request.Method, route, c.Writer.Status(), time.Since(started))
	}
}

func RecordHTTPSpanAttributes() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		span := trace.SpanFromContext(c.Request.Context())
		if !span.SpanContext().IsValid() {
			return
		}
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		span.SetAttributes(
			attribute.String("soj.request_id", c.GetString(ContextRequestID)),
			attribute.String("soj.http.route", route),
			attribute.Int("soj.http.status_code", c.Writer.Status()),
		)
		if c.Writer.Status() >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, "http_5xx")
		}
	}
}

func noopMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
}

func AbortUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, Envelope{
		Data:      nil,
		Error:     &ErrorBody{Code: "unauthorized", Message: "unauthorized"},
		RequestID: requestID(c),
	})
}
