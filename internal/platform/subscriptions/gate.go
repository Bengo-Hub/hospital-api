package subscriptions

import (
	"fmt"
	"net/http"
	"time"

	authclient "github.com/Bengo-Hub/shared-auth-client"
)

const (
	gracePeriodDays = 7
	upgradeURL      = "/settings?tab=subscription"
)

// exempt reports whether the request's token bypasses all subscription gating (platform
// owners, explicitly subscription-exempt tenants, demo tenants). Delegates to the shared
// claims helper so every gate path stays consistent.
func exempt(r *http.Request) bool {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		return true // no claims (e.g. S2S paths) — don't block here
	}
	return claims.IsGatingExempt()
}

// SubscriptionGate enforces an active subscription on mutating requests only (POST/PUT/
// PATCH/DELETE) — matching every other domain service per hospital-api/docs/integrations.md
// §4 ("Mutations-only subscription enforcement"). GET/HEAD/OPTIONS always pass so a lapsed
// subscription never blocks read access to existing clinical data.
func SubscriptionGate() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}

			claims, ok := authclient.ClaimsFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			if claims.IsGatingExempt() {
				next.ServeHTTP(w, r)
				return
			}

			switch claims.SubscriptionStatus {
			case "ACTIVE", "TRIAL", "":
				next.ServeHTTP(w, r)
				return
			case "EXPIRED":
				if claims.SubscriptionExpires != nil {
					expAt := claims.ExpiresAt()
					if expAt != nil {
						deadline := expAt.Add(gracePeriodDays * 24 * time.Hour)
						if time.Now().Before(deadline) {
							daysLeft := int(time.Until(deadline).Hours()/24) + 1
							w.Header().Set("X-Sub-Grace-Days-Left", fmt.Sprintf("%d", daysLeft))
							next.ServeHTTP(w, r)
							return
						}
					}
				}
				writeSubscriptionError(w, true)
				return
			default:
				writeSubscriptionError(w, false)
				return
			}
		})
	}
}

// RequireFeature gates a route group on a subscription feature code. Exempt tokens pass.
// The feature code must be a real code seeded by subscriptions-api (see features.go).
func RequireFeature(featureCode string) func(http.Handler) http.Handler {
	return authclient.RequireFeatureCode(featureCode)
}

// CheckStructuralLimit enforces a hard-block structural cap before creating a new resource.
// Returns true when the request may proceed, or writes a structured 402 and returns false.
func CheckStructuralLimit(w http.ResponseWriter, r *http.Request, metric, limitKey string, currentCount int) bool {
	if exempt(r) {
		return true
	}
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok {
		return true
	}
	limit := claims.GetLimit(limitKey)
	if limit <= 0 {
		return true // 0 = not configured, -1 = unlimited
	}
	if currentCount >= limit {
		writeLimitReached(w, metric, limit, currentCount)
		return false
	}
	return true
}

func writeLimitReached(w http.ResponseWriter, metric string, limit, used int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	_, _ = w.Write([]byte(fmt.Sprintf(
		`{"code":"usage_limit_exceeded","error":"usage_limit_exceeded","message":"You've reached your plan's %s limit (%d).","metric":%q,"limit":%d,"used":%d,"upgrade_url":%q}`,
		metric, limit, metric, limit, used, upgradeURL)))
}

func writeSubscriptionError(w http.ResponseWriter, gracePeriodEnded bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	_, _ = w.Write([]byte(fmt.Sprintf(
		`{"code":"subscription_expired","error":"subscription_expired","grace_period_ended":%t,"upgrade_url":%q}`,
		gracePeriodEnded, upgradeURL)))
}
