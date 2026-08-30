package identity

import "testing"

func TestMapSSORoleToHospital(t *testing.T) {
	cases := []struct {
		name  string
		roles []string
		want  string
	}{
		{"admin wins wildcard", []string{"admin"}, "admin"},
		{"superuser maps to admin", []string{"superuser"}, "admin"},
		{"manager", []string{"manager"}, "manager"},
		{"doctor", []string{"doctor"}, "doctor"},
		{"clinician alias maps to doctor", []string{"clinician"}, "doctor"},
		{"physician alias maps to doctor", []string{"physician"}, "doctor"},
		{"nurse", []string{"nurse"}, "nurse"},
		{"pharmacist", []string{"pharmacist"}, "pharmacist"},
		{"records_clerk", []string{"records_clerk"}, "records_clerk"},
		{"receptionist alias maps to records_clerk", []string{"receptionist"}, "records_clerk"},
		{"cashier", []string{"cashier"}, "cashier"},
		{"first recognised role wins when multiple present", []string{"customer", "doctor"}, "doctor"},
		{"unrecognised role maps to empty (no-op)", []string{"customer"}, ""},
		{"empty roles maps to empty (no-op)", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapSSORoleToHospital(tc.roles)
			if got != tc.want {
				t.Errorf("mapSSORoleToHospital(%v) = %q, want %q", tc.roles, got, tc.want)
			}
		})
	}
}

func TestExtractRoles(t *testing.T) {
	t.Run("[]string claim shape", func(t *testing.T) {
		claims := map[string]any{"roles": []string{"doctor", "admin"}}
		got := extractRoles(claims)
		if len(got) != 2 || got[0] != "doctor" || got[1] != "admin" {
			t.Errorf("extractRoles(%v) = %v, want [doctor admin]", claims, got)
		}
	})

	t.Run("[]interface{} claim shape (JSON-decoded JWT)", func(t *testing.T) {
		claims := map[string]any{"roles": []interface{}{"nurse", "manager"}}
		got := extractRoles(claims)
		if len(got) != 2 || got[0] != "nurse" || got[1] != "manager" {
			t.Errorf("extractRoles(%v) = %v, want [nurse manager]", claims, got)
		}
	})

	t.Run("[]interface{} with a non-string element is skipped, not panicked", func(t *testing.T) {
		claims := map[string]any{"roles": []interface{}{"doctor", 42, "nurse"}}
		got := extractRoles(claims)
		if len(got) != 2 || got[0] != "doctor" || got[1] != "nurse" {
			t.Errorf("extractRoles(%v) = %v, want [doctor nurse] (non-string dropped)", claims, got)
		}
	})

	t.Run("missing roles claim returns nil, not a panic", func(t *testing.T) {
		got := extractRoles(map[string]any{})
		if got != nil {
			t.Errorf("extractRoles(empty) = %v, want nil", got)
		}
	})

	t.Run("wrong-typed roles claim returns nil, not a panic", func(t *testing.T) {
		got := extractRoles(map[string]any{"roles": "doctor"})
		if got != nil {
			t.Errorf("extractRoles(string roles) = %v, want nil", got)
		}
	})
}

func TestIsHospitalRelevant(t *testing.T) {
	hospital := "hospital"
	hospitality := "hospitality"
	cases := []struct {
		name           string
		outletUseCase  string
		outletResolved bool
		tenantUseCase  *string
		roles          []string
		want           bool
	}{
		{"outlet resolved hospital -> allow regardless of role", "hospital", true, nil, []string{"manager"}, true},
		{
			name: "outlet resolved non-hospital -> block even for an otherwise-unambiguous role " +
				"(demo cashier/manager/admin assigned to a POS outlet, never demo-hospital)",
			outletUseCase: "hospitality", outletResolved: true, tenantUseCase: nil,
			roles: []string{"manager"}, want: false,
		},
		{"outlet resolved non-hospital blocks doctor too (shouldn't happen in practice, but the outlet wins)",
			"retail", true, nil, []string{"doctor"}, false},
		{"no outlet + unambiguous hospital role -> allow", "", false, nil, []string{"doctor"}, true},
		{"no outlet + nurse -> allow", "", false, nil, []string{"nurse"}, true},
		{"no outlet + pharmacist -> allow", "", false, nil, []string{"pharmacist"}, true},
		{"no outlet + records_clerk -> allow", "", false, nil, []string{"records_clerk"}, true},
		{"no outlet + ambiguous admin role + non-hospital tenant -> block", "", false, &hospitality, []string{"admin"}, false},
		{"no outlet + ambiguous manager role + no tenant use_case known -> block", "", false, nil, []string{"manager"}, false},
		{"no outlet + ambiguous admin role + single-vertical hospital tenant -> allow (founding admin)", "", false, &hospital, []string{"admin"}, true},
		{"no outlet, no role, no tenant use_case -> block", "", false, nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isHospitalRelevant(tc.outletUseCase, tc.outletResolved, tc.tenantUseCase, tc.roles)
			if got != tc.want {
				t.Errorf("isHospitalRelevant(%q, %v, %v, %v) = %v, want %v",
					tc.outletUseCase, tc.outletResolved, tc.tenantUseCase, tc.roles, got, tc.want)
			}
		})
	}
}

// NOTE: EnsureUserFromToken's full JIT-provisioning + role-healing flow (the "user already
// exists -> still re-run role assignment" contract documented on that function) touches the
// ent client and is not covered by an automated test in this pass — hospital-api has no
// sqlite/in-memory ent driver wired for DB-free service tests (unlike e.g. pos-api's
// promotions package, which extracts a pure decision function specifically to avoid this).
// The pure role-mapping/claims-parsing logic above IS covered. A follow-up should either add
// an ent sqlite test driver or an integration test against the local Postgres dev DB.
