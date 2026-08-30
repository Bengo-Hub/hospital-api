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
	entuseroutlet "github.com/bengobox/hospital-service/internal/ent/hospitaluseroutlet"
)

// LocalUserResolver is the subset of identity.Service used here. Declared as an interface (not
// imported directly as *identity.Service in the signature below — it still is, see
// OutletContextMiddleware's param, but this documents the exact contract) so the enforcement
// logic only depends on ResolveLocalUserID's behavior, including its deactivation check (a
// non-active user resolves to an error here too, consistent with the rest of the RBAC layer).
type LocalUserResolver interface {
	ResolveLocalUserID(ctx context.Context, tenantID, authUserID uuid.UUID) (uuid.UUID, error)
}

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
// Per-user outlet assignment enforcement (2026-08-30, mirrors inventory-api's UserOutlet/
// EnforceOutletAssignment — the fleet's proven pattern; pos-api's StaffOutlet and erp-api's port
// of the same Django middleware do the equivalent): a resolved outlet from step 1 or 2 is
// validated against the caller's HospitalUserOutlet assignments UNLESS they can access all
// outlets (platform owner / admin-level / HQ user, per shared-auth-client's
// Claims.CanAccessAllOutlets()). Progressive rollout: a user with ZERO assignment rows is
// treated as unrestricted (matches inventory-api's exact carve-out) so existing staff aren't
// locked out before a tenant admin assigns their outlets via the new /users/{id}/outlets API.
// Step 3 (the tenant-HQ default) is intentionally NOT validated — it only ever fires when
// neither an explicit header nor a JWT claim resolved anything, i.e. no real selection was made.
func OutletContextMiddleware(client *ent.Client, log *zap.Logger, resolver LocalUserResolver) func(http.Handler) http.Handler {
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

			// Enforce per-user outlet assignment for an explicit selection (header or JWT claim) —
			// never for the HQ default below, since that only fires when no real selection exists.
			if resolved != nil && hasClaims && !claims.CanAccessAllOutlets() && !claims.IsService {
				authUserID, err := claims.UserID()
				if err == nil {
					localUserID, resErr := resolver.ResolveLocalUserID(ctx, tenantID, authUserID)
					if resErr == nil {
						assigned, checkErr := client.HospitalUserOutlet.Query().
							Where(entuseroutlet.TenantIDEQ(tenantID), entuseroutlet.UserIDEQ(localUserID)).
							All(ctx)
						if checkErr != nil {
							log.Warn("outlet assignment check failed; allowing request", zap.Error(checkErr))
						} else if len(assigned) > 0 {
							allowed := false
							for _, a := range assigned {
								if a.OutletID == resolved.ID {
									allowed = true
									break
								}
							}
							if !allowed {
								w.Header().Set("Content-Type", "application/json")
								w.WriteHeader(http.StatusForbidden)
								_, _ = w.Write([]byte(`{"error":"OUTLET_NOT_ASSIGNED","message":"You are not assigned to this outlet"}`))
								return
							}
						}
						// len(assigned) == 0: progressive rollout — unrestricted until an admin assigns.
					}
					// resErr != nil (not provisioned / deactivated): fall through unrestricted here —
					// RequireServicePermission (which runs on every gated route) is the real
					// authorization backstop and will already reject a deactivated/unprovisioned user.
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
