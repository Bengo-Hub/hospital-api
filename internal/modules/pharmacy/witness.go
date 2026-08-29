package pharmacy

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent/hospitaluser"
	"github.com/bengobox/hospital-service/internal/modules/authapi"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
)

// This file closes a real security/compliance gap: Dispense used to take
// DispenseLineInput.WitnessStaffID straight from the client-supplied request body (only
// null-checked, never identity-verified) and write it verbatim into
// ControlledSubstanceLog.witness_staff_id — the compliance record for controlled-substance
// dual-witness dispensing. Any dispensing user could name ANY staff UUID as the "witness" with
// zero verification that person actually witnessed anything.
//
// Fix, modelled on pos-api's step_up.go/pharmacy_controlled.go pattern (short-lived HS256
// "approval token" issued after a verified credential check, consumed by the sensitive-action
// handler) but adapted to hospital-api's actual credential source: hospital-api has NO local PIN
// infrastructure (pos-api's StaffMember.pin_hash has no equivalent here), so the witness instead
// re-authenticates with their own email+password, verified server-side against auth-api's
// existing, already-public POST /auth/login (see authapi.Client). VerifyWitness (below)
// orchestrates that credential check + identity/tenant/permission verification and mints a
// short-lived witnessToken; Dispense (service.go) trusts ONLY that verified token, never the
// request body — mirroring pos-api's own comment: "witness_staff_id is intentionally NOT
// re-read from input... Trusting the request body would let a client claim any witness without
// actually authorizing them."

const (
	// witnessTokenPurpose scopes the token to this one action, exactly like pos-api's
	// approvalClaims.Action does for its step-up tokens.
	witnessTokenPurpose = "pharmacy.controlled_substance_witness"
	// witnessTokenTTL mirrors pos-api's ~2-minute step-up token lifetime.
	witnessTokenTTL = 2 * time.Minute
)

// witnessClaims is the short-lived, single-purpose token minted by VerifyWitness and consumed
// by Dispense. WitnessUserID is HospitalUser.ID — which, per identity.Service.EnsureUserFromToken
// (SetID(authServiceID)), is always equal to WitnessAuthID; both are carried for clarity/
// auditability, not because they can differ.
type witnessClaims struct {
	Purpose       string `json:"purpose"`
	WitnessUserID string `json:"witness_user_id"`
	WitnessAuthID string `json:"witness_auth_id"`
	TenantID      string `json:"tenant_id"`
	jwt.RegisteredClaims
}

// issueWitnessToken signs a short-lived HS256 token authorizing witnessUserID (an ALREADY
// verified HospitalUser) as the dual-witness for one dispense action within tenantID.
func issueWitnessToken(witnessUserID, witnessAuthID, tenantID uuid.UUID, secret []byte) (string, error) {
	now := time.Now()
	claims := witnessClaims{
		Purpose:       witnessTokenPurpose,
		WitnessUserID: witnessUserID.String(),
		WitnessAuthID: witnessAuthID.String(),
		TenantID:      tenantID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "hospital-witness-verify",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(witnessTokenTTL)),
			ID:        uuid.New().String(),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// verifyWitnessToken validates a witness token for tenantID and returns the VERIFIED witness's
// HospitalUser ID. ok is false when the token is missing, invalid, expired, issued for a
// different purpose, or issued for a different tenant. Dispense must use ONLY this returned
// identity — never anything client-supplied.
func verifyWitnessToken(tokenStr string, tenantID uuid.UUID, secret []byte) (witnessUserID uuid.UUID, ok bool) {
	if tokenStr == "" || len(secret) == 0 {
		return uuid.Nil, false
	}
	claims := &witnessClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil || !token.Valid || claims.Purpose != witnessTokenPurpose {
		return uuid.Nil, false
	}
	if claims.TenantID != tenantID.String() {
		return uuid.Nil, false
	}
	id, perr := uuid.Parse(claims.WitnessUserID)
	if perr != nil || id == uuid.Nil {
		return uuid.Nil, false
	}
	return id, true
}

// VerifyWitnessRequest is the input to VerifyWitness — the witness's OWN credentials, never the
// dispensing user's.
type VerifyWitnessRequest struct {
	Email    string
	Password string
	TOTPCode string
}

// VerifyWitnessResult is either an MFA challenge to relay back to the caller unchanged (status
// 200, same shape as auth-api's own mfa_required response, so the frontend can prompt for the
// code and resubmit), or a minted short-lived witness token proving a real, distinct, permitted
// staff member re-authenticated as the controlled-substance dual witness.
type VerifyWitnessResult struct {
	MFARequired bool
	MFAMethod   string
	MFAUserID   string

	WitnessToken string
	WitnessName  string
	ExpiresIn    int
}

// VerifyWitness re-authenticates a controlled-substance dispense witness against auth-api's own
// login, then verifies the resulting identity is a real, already-provisioned hospital-api staff
// member of THIS tenant, distinct from the calling/dispensing user, holding pharmacy-dispensing
// (or manager-tier) permission — and only then mints a short-lived witnessToken. Every rejection
// path returns an error meant to be surfaced as 403 (never an ambiguous fallback — this gates a
// compliance record).
func (s *Service) VerifyWitness(ctx context.Context, tenantID uuid.UUID, tenantSlug string, callingAuthUserID uuid.UUID, req VerifyWitnessRequest) (*VerifyWitnessResult, error) {
	if s.authAPI == nil || !s.authAPI.Enabled() {
		return nil, fmt.Errorf("pharmacy: witness verification is not configured")
	}
	if req.Email == "" || req.Password == "" {
		return nil, fmt.Errorf("pharmacy: witness email and password are required")
	}
	if tenantSlug == "" {
		return nil, fmt.Errorf("pharmacy: tenant context required")
	}

	loginResult, status, err := s.authAPI.Login(ctx, authapi.LoginRequest{
		Email:      req.Email,
		Password:   req.Password,
		TenantSlug: tenantSlug,
		TOTPCode:   req.TOTPCode,
	})
	if err != nil {
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return nil, fmt.Errorf("pharmacy: witness credentials are invalid")
		}
		s.log.Warn("witness login call failed", zap.Error(err))
		return nil, fmt.Errorf("pharmacy: witness verification is temporarily unavailable")
	}

	if loginResult.MFARequired {
		return &VerifyWitnessResult{
			MFARequired: true,
			MFAMethod:   loginResult.MFAMethod,
			MFAUserID:   loginResult.UserID,
		}, nil
	}
	if loginResult.AccessToken == "" {
		return nil, fmt.Errorf("pharmacy: witness verification did not return a token")
	}

	if s.authValidator == nil {
		return nil, fmt.Errorf("pharmacy: witness token validation is not configured")
	}
	claims, verr := s.authValidator.ValidateToken(loginResult.AccessToken)
	if verr != nil {
		// A token minted by the login call we just made failing our OWN validator would be
		// unexpected (bad JWKS config, clock skew, etc.) — reject rather than trust it.
		s.log.Warn("witness access token failed validation", zap.Error(verr))
		return nil, fmt.Errorf("pharmacy: witness verification failed")
	}

	witnessAuthID, perr := uuid.Parse(claims.Subject)
	if perr != nil || witnessAuthID == uuid.Nil {
		return nil, fmt.Errorf("pharmacy: witness verification failed")
	}

	// The witness must belong to THIS tenant — reject on any mismatch.
	witnessTenantID, terr := uuid.Parse(claims.TenantID)
	if terr != nil || witnessTenantID != tenantID {
		return nil, fmt.Errorf("pharmacy: witness must belong to this tenant")
	}

	// Dual-witness means two DIFFERENT people. Compare on the verified auth identity, not
	// anything client-supplied.
	if witnessAuthID == callingAuthUserID {
		return nil, fmt.Errorf("pharmacy: the witness must be a different staff member than the person dispensing")
	}

	// The witness must be a real, already-provisioned hospital-api staff member of this
	// tenant — not just any auth-api account. Mirrors identity.Service's own lookup.
	witnessUser, uerr := s.client.HospitalUser.Query().
		Where(hospitaluser.AuthServiceUserIDEQ(witnessAuthID), hospitaluser.TenantID(tenantID)).
		Only(ctx)
	if uerr != nil {
		return nil, fmt.Errorf("pharmacy: witness is not a provisioned staff member for this tenant")
	}

	if s.rbac == nil {
		return nil, fmt.Errorf("pharmacy: witness permission check is not configured")
	}
	allowed, aerr := s.rbac.HasAnyPermission(ctx, tenantID, witnessUser.ID, rbac.PermPharmacyDispense, rbac.PermPharmacyManage)
	if aerr != nil {
		s.log.Warn("witness permission check failed", zap.Error(aerr))
	}
	if !allowed && len(claims.Roles) > 0 {
		allowed, aerr = s.rbac.HasAnyPermissionViaGlobalRoles(ctx, tenantID, claims.Roles, rbac.PermPharmacyDispense, rbac.PermPharmacyManage)
		if aerr != nil {
			s.log.Warn("witness global-role permission check failed", zap.Error(aerr))
		}
	}
	if !allowed {
		return nil, fmt.Errorf("pharmacy: witness does not hold pharmacy-dispensing permission")
	}

	if len(s.witnessSecret) == 0 {
		return nil, fmt.Errorf("pharmacy: witness token signing is not configured")
	}
	token, ierr := issueWitnessToken(witnessUser.ID, witnessAuthID, tenantID, s.witnessSecret)
	if ierr != nil {
		s.log.Error("issue witness token failed", zap.Error(ierr))
		return nil, fmt.Errorf("pharmacy: internal error")
	}

	return &VerifyWitnessResult{
		WitnessToken: token,
		WitnessName:  witnessUser.Name,
		ExpiresIn:    int(witnessTokenTTL.Seconds()),
	}, nil
}
