package httpapi

import (
	"context"
	"net/http"

	"SOJ/internal/apperror"

	"github.com/gin-gonic/gin"
)

type ReadyCheck func(context.Context) error

type RouterOptions struct {
	Middleware MiddlewareSet
	ReadyCheck ReadyCheck
	Modules    []Module
}

func NewRouter(opts RouterOptions) *gin.Engine {
	router := gin.New()

	middleware := opts.Middleware
	if middleware.RequestID == nil {
		middleware = DefaultMiddlewareSet()
	}
	router.Use(middleware.RequestID)
	if middleware.CORS != nil {
		router.Use(middleware.CORS)
	}
	if middleware.RateLimit != nil {
		router.Use(middleware.RateLimit)
	}
	if middleware.Auth != nil {
		router.Use(middleware.Auth)
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
