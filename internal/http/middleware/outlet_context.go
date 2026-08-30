package middleware

import (
	"context"
	"net/http"

	"github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	entoutlet "github.com/bengobox/hospital-service/internal/ent/outlet"
)

type outletContextKey struct{}

// OutletContext carries the resolved outlet for the current request.
type OutletContext struct {
	ID      uuid.UUID
	Code    string
	Name    string
	UseCase string
	IsHQ    bool
	Status  string
}

// OutletFromContext retrieves the resolved outlet from the request context.
func OutletFromContext(ctx context.Context) *OutletContext {
	v, _ := ctx.Value(outletContextKey{}).(*OutletContext)
	return v
}

// OutletContextMiddleware resolves the active outlet for the request and injects it into
// the context. Resolution order mirrors pos-api's TenantContext middleware:
//
//  1. X-Outlet-ID header    — explicit override (HQ user / frontend-selected outlet)
//  2. JWT claim's outlet_id — sessions whose token already carries the outlet they logged into
//  3. Tenant's HQ outlet    — fallback when no outlet can be determined
//
// Non-HQ per-user outlet assignment enforcement (pos-api's StaffOutlet check) is deferred:
// hospital-api has no clinical/staff-outlet schema yet (Sprint 4, out of scope for this
// pass) so there is no local table to check membership against. Any authenticated user in
// the tenant may currently select any of the tenant's outlets via X-Outlet-ID; add a
// UserOutlet-equivalent projection + membership check here once that schema lands.
func OutletContextMiddleware(client *ent.Client, log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			claims, hasClaims := authclient.ClaimsFromContext(ctx)

			// Prefer the request's RESOLVED tenant (TenantV2 + the JIT tenant-sync middleware in
			// router.go) over the caller's own JWT-embedded tenant — those differ for a platform
			// owner visiting a tenant other than their own, and using claims.TenantID directly
			// here would silently resolve outlets for the WRONG tenant in that case.
			var tenantID uuid.UUID
			if raw := httpware.GetTenantID(ctx); raw != "" {
				if id, err := uuid.Parse(raw); err == nil {
					tenantID = id
				}
			}
			if tenantID == uuid.Nil && hasClaims && claims.TenantID != "" {
				if id, err := uuid.Parse(claims.TenantID); err == nil {
					tenantID = id
				}
			}
			if tenantID == uuid.Nil {
				next.ServeHTTP(w, r)
				return
			}

			var requestedOutletID uuid.UUID
			if raw := r.Header.Get(httpware.HeaderOutletID); raw != "" {
				if id, err := uuid.Parse(raw); err == nil {
					requestedOutletID = id
				}
			}

			var resolved *OutletContext

			if requestedOutletID != uuid.Nil {
				o, err := client.Outlet.Query().
					Where(entoutlet.ID(requestedOutletID), entoutlet.TenantID(tenantID)).
					Only(ctx)
				if err != nil {
					http.Error(w, `{"error":"outlet not found or access denied"}`, http.StatusForbidden)
					return
				}
				resolved = outletToCtx(o)
			}

			// Fallback: the caller's own assigned outlet from their JWT claim.
			if resolved == nil && hasClaims {
				if raw := claims.GetOutletID(); raw != "" {
					if id, err := uuid.Parse(raw); err == nil {
						o, err := client.Outlet.Query().
							Where(entoutlet.ID(id), entoutlet.TenantID(tenantID)).
							Only(ctx)
						if err == nil {
							resolved = outletToCtx(o)
						}
					}
				}
			}

			// Fallback: HQ outlet for this tenant.
			if resolved == nil {
				o, err := client.Outlet.Query().
					Where(entoutlet.TenantID(tenantID), entoutlet.IsHq(true)).
					First(ctx)
				if err == nil {
					resolved = outletToCtx(o)
				}
			}

			if resolved != nil {
				ctx = context.WithValue(ctx, outletContextKey{}, resolved)
				ctx = httpware.WithOutletID(ctx, resolved.ID.String())
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func outletToCtx(o *ent.Outlet) *OutletContext {
	useCase := ""
	if o.UseCase != nil {
		useCase = *o.UseCase
	}
	return &OutletContext{
		ID:      o.ID,
		Code:    o.Code,
		Name:    o.Name,
		UseCase: useCase,
		IsHQ:    o.IsHq,
		Status:  o.Status,
	}
}
