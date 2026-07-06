package httpapi

import (
	"context"
	"net/http"
	"time"

	"SOJ/internal/apperror"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

type ReadyCheck func(context.Context) error

type RouterOptions struct {
	Middleware     MiddlewareSet
	ReadyCheck     ReadyCheck
	Modules        []Module
	Metrics        HTTPMetrics
	TracingEnabled bool
	TracingService string
}

type HTTPMetrics interface {
	Handler() http.Handler
	ObserveHTTPRequest(method, route string, status int, duration time.Duration)
}

func NewRouter(opts RouterOptions) *gin.Engine {
	router := gin.New()

	middleware := opts.Middleware
	if middleware.RequestID == nil {
		middleware = DefaultMiddlewareSet()
	}
	router.Use(middleware.RequestID)
	if opts.TracingEnabled {
		service := opts.TracingService
		if service == "" {
			service = "soj"
		}
		router.Use(otelgin.Middleware(service))
		router.Use(RecordHTTPSpanAttributes())
	}
	if middleware.CORS != nil {
		router.Use(middleware.CORS)
	}
	if middleware.RateLimit != nil {
		router.Use(middleware.RateLimit)
	}
	if middleware.Auth != nil {
		router.Use(middleware.Auth)
	}
	if opts.Metrics != nil {
		router.Use(RecordHTTPMetrics(opts.Metrics))
		router.GET("/metrics", gin.WrapH(opts.Metrics.Handler()))
	}
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		OK(c, gin.H{"status": "ok"})
	})
	router.GET("/readyz", func(c *gin.Context) {
		if opts.ReadyCheck != nil {
			if err := opts.ReadyCheck(c.Request.Context()); err != nil {
				RenderError(c, apperror.ServiceUnavailable("service unavailable"))
				return
			}
		}
		OK(c, gin.H{"status": "ready"})
	})

	api := router.Group("/api/v1")
	for _, module := range opts.Modules {
		if module != nil {
			module.RegisterRoutes(api)
		}
	}

	router.NoRoute(func(c *gin.Context) {
		RenderError(c, apperror.NotFound("route.not_found", http.StatusText(http.StatusNotFound)))
	})

	return router
}
