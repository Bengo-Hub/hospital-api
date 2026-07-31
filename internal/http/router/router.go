package router

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"

	httpware "github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"

	handlers "github.com/bengobox/hospital-service/internal/http/handlers"
)

// Deps bundles everything the router mounts. Sprint 0+ adds real domain
// handlers here (Patient, Visit, Triage, ...) alongside Health/Ping.
type Deps struct {
	Log            *zap.Logger
	Health         *handlers.HealthHandler
	AuthMiddleware *authclient.AuthMiddleware
	AllowedOrigins []string
}

// New builds the chi router with the standard platform middleware stack.
func New(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(httpware.RequestID)
	r.Use(httpware.Logging(d.Log))
	r.Use(httpware.Recover(d.Log))
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   d.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Origin", "X-Request-ID", "X-Tenant-ID", "X-Tenant-Slug", "X-API-Key", "Idempotency-Key", "X-Outlet-ID"},
		ExposedHeaders:   []string{"Link", "Retry-After"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/healthz", d.Health.Liveness)
	r.Get("/readyz", d.Health.Readiness)
	r.Get("/metrics", d.Health.Metrics)

	r.Route("/api/v1/{tenant}/hospital", func(r chi.Router) {
		r.Use(d.AuthMiddleware.RequireAuth)
		// Placeholder route proving JWKS auth wiring works end to end.
		// Sprint 0+ mounts real domain routes (patients, visits, triage, ...) here.
		r.Get("/ping", handlers.Ping)
	})

	return r
}
