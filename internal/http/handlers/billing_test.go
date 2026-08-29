package handlers

import (
	"testing"

	"github.com/bengobox/hospital-service/internal/modules/rbac"
)

func TestSourceModulePermission(t *testing.T) {
	cases := []struct {
		module string
		want   string
	}{
		{"records", rbac.PermRecordsAdd},
		{"reception", rbac.PermReceptionAdd},
		{"triage", rbac.PermTriageAdd},
		{"consultation", rbac.PermConsultationAdd},
		{"lab", rbac.PermLabAdd},
		{"pharmacy", rbac.PermPharmacyDispense},
		{"theatre", rbac.PermTheatreAdd},
		{"inpatient", rbac.PermInpatientAdd},
		{"unknown_department", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := sourceModulePermission(c.module); got != c.want {
			t.Errorf("sourceModulePermission(%q) = %q, want %q", c.module, got, c.want)
		}
	}
}
