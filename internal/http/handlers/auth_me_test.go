package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/golang-jwt/jwt/v5"
)

// TestGetMe_DoesNotLeakRawJWTPermissions guards against a real bug found via live curl
// testing: the JWT's own "permissions" claim is a cross-vertical dump (every permission the
// caller's global SSO role maps to across pos/inventory/ordering/logistics/treasury/auth, not
// just hospital-service). GetMe must never surface that raw claim — only local hospital RBAC
// results — otherwise hospital-ui's useAppPermissions client-side ROLE_PERMISSIONS fallback
// (which only engages when the server array is empty) gets silently defeated by a non-empty
// but entirely irrelevant permissions array.
func TestGetMe_DoesNotLeakRawJWTPermissions(t *testing.T) {
	h := NewAuthMeHandler(nil) // no local RBAC service wired -> local enrichment is a no-op

	claims := &authclient.Claims{
		Email:       "admin@demo.codevertexafrica.com",
		TenantID:    "11111111-1111-1111-1111-111111111111",
		TenantSlug:  "codevertex-demo",
		Roles:       []string{"admin"},
		Permissions: []string{"pos.sale.create", "inventory.item.view", "treasury.invoice.view"},
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "22222222-2222-2222-2222-222222222222",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/codevertex-demo/hospital/auth/me", nil)
	req = req.WithContext(authclient.ContextWithClaims(req.Context(), claims))
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GetMe status = %d, want %d", rec.Code, http.StatusOK)
	}

	var out struct {
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(out.Permissions) != 0 {
		t.Errorf("permissions = %v, want empty (raw JWT cross-vertical claim must not leak through)", out.Permissions)
	}
	if len(out.Roles) != 1 || out.Roles[0] != "admin" {
		t.Errorf("roles = %v, want [admin] (JWT roles must still pass through unchanged)", out.Roles)
	}
}
