package authapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// InviteMemberRequest mirrors auth-api's addTenantMemberRequest (admin_handler.go) — the
// subset hospital-api's invite-staff action actually sets. Roles/OutletID drive
// auth_events.go's handleUserCreated JIT role-assignment: a hospital role name (or an
// outlet_id resolving to a hospital outlet) is enough for the invited user's hospital-api role
// to be assigned automatically the moment they first log in — no separate
// "pending assignment" mechanism is needed.
type InviteMemberRequest struct {
	Email    string   `json:"email"`
	Name     string   `json:"name,omitempty"`
	Roles    []string `json:"roles"`
	OutletID string   `json:"outlet_id,omitempty"`
}

// InviteMemberResult mirrors auth-api's tenantMemberResponse (only the fields hospital-api
// relays to its own caller).
type InviteMemberResult struct {
	UserID string `json:"user_id"`
	Status string `json:"status"`
	// TempPassword is returned ONCE when this call created a brand-new auth-api account — relay
	// it to the inviting admin for a one-time reveal, never log or persist it.
	TempPassword string `json:"temp_password,omitempty"`
}

// InviteTenantMember calls auth-api's S2S POST /api/v1/s2s/tenants/{tenant_id}/members —
// find-or-create the auth-api user by email and attach/update the tenant membership with the
// given roles/outlet. This is the SAME mechanism every other service's own staff-invite flow
// uses (see pos-api's staff.go provisionAuthMember) — hospital-api never mints its own user
// identity.
func (c *Client) InviteTenantMember(ctx context.Context, tenantID uuid.UUID, req InviteMemberRequest) (*InviteMemberResult, error) {
	if !c.enabled || c.apiKey == "" {
		return nil, fmt.Errorf("authapi client not configured (AUTH_SERVICE_URL / INTERNAL_SERVICE_KEY)")
	}
	resp, err := c.sc.Post(ctx, "/api/v1/s2s/tenants/"+tenantID.String()+"/members", req, map[string]string{"X-API-Key": c.apiKey})
	if err != nil {
		return nil, fmt.Errorf("authapi: invite member: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("authapi: invite member: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out InviteMemberResult
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("authapi: decode invite member result: %w", err)
	}
	return &out, nil
}
