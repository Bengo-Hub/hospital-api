package router

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"

	httpware "github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/ent"
	handlers "github.com/bengobox/hospital-service/internal/http/handlers"
	outletmw "github.com/bengobox/hospital-service/internal/http/middleware"
	"github.com/bengobox/hospital-service/internal/modules/identity"
	rbacmodule "github.com/bengobox/hospital-service/internal/modules/rbac"
	"github.com/bengobox/hospital-service/internal/platform/subscriptions"
)

// Deps bundles everything the router mounts. Sprint 4+ adds real domain
// handlers here (Patient, Visit, Triage, ...) alongside Health/Ping/AuthMe.
type Deps struct {
	Log            *zap.Logger
	Health         *handlers.HealthHandler
	AuthMiddleware *authclient.AuthMiddleware
	AllowedOrigins []string

	// Trinity Authorization plumbing (nil-safe: each stage no-ops if its dep is nil, so the
	// router still boots in a partially-wired test/dev environment).
	EntClient   *ent.Client
	IdentitySvc *identity.Service
	RBACSvc     *rbacmodule.Service
	AuthMe      *handlers.AuthMeHandler
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
		// ── Protected endpoints (auth required) ──────────────────────────────
		r.Group(func(prot chi.Router) {
			if d.AuthMiddleware != nil {
				prot.Use(d.AuthMiddleware.RequireAuth)
				// Mutations-only subscription gate (docs/integrations.md §4).
				prot.Use(subscriptions.SubscriptionGate())
			}

			// JIT provisioning: create/heal the local HospitalUser + role assignment on
			// every authenticated request, before tenant/outlet context is resolved.
			if d.IdentitySvc != nil {
				prot.Use(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						claims, ok := authclient.ClaimsFromContext(r.Context())
						if ok && claims.Subject != "" {
							subject, _ := uuid.Parse(claims.Subject)
							slug := claims.GetTenantSlug()
							if slug != "" {
								_, err := d.IdentitySvc.EnsureUserFromToken(r.Context(), subject, slug, map[string]any{
									"email":             claims.Email,
									"full_name":         claims.Email,
									"roles":             claims.Roles,
									"permissions":       claims.Permissions,
									"is_platform_owner": claims.IsPlatformOwner,
									"outlet_id":         claims.GetOutletID(),
								})
								if err != nil {
									d.Log.Warn("jit provisioning failed", zap.Error(err))
								}
							}
						}
						next.ServeHTTP(w, r)
					})
				})
			}

			// Tenant param + claims-tenant match (existing scaffold already routes on
			// "{tenant}", not "{tenantID}" — keep the one URL segment, don't double it up).
			prot.Use(httpware.TenantV2(httpware.TenantConfig{
				ClaimsExtractor: func(ctx context.Context) (tenantID, tenantSlug string, isPlatformOwner bool, ok bool) {
					claims, found := authclient.ClaimsFromContext(ctx)
					if !found {
						return "", "", false, false
					}
					return claims.TenantID, claims.GetTenantSlug(), claims.IsPlatformOwner, true
				},
				URLParamFunc: chi.URLParam,
				URLParamName: "tenant",
				Required:     true,
			}))
			if d.EntClient != nil {
				prot.Use(outletmw.OutletContextMiddleware(d.EntClient, d.Log))
			}

			if d.AuthMe != nil {
				prot.Get("/auth/me", d.AuthMe.GetMe)
			}

			// Placeholder route proving JWKS auth + Trinity middleware chain works end to
			// end. Sprint 4+ mounts real domain routes (patients, visits, triage, ...) here,
			// each gated with outletmw.RequireServicePermission(d.RBACSvc, ...).
			prot.Get("/ping", handlers.Ping)
		})
	})

	return r
}
