package rbac

import "testing"

func TestHasUnambiguousHospitalRole(t *testing.T) {
	cases := []struct {
		name  string
		roles []string
		want  bool
	}{
		{"doctor is hospital-exclusive", []string{"doctor"}, true},
		{"clinician alias is hospital-exclusive", []string{"clinician"}, true},
		{"physician alias is hospital-exclusive", []string{"physician"}, true},
		{"nurse is hospital-exclusive", []string{"nurse"}, true},
		{"pharmacist is hospital-exclusive", []string{"pharmacist"}, true},
		{"records_clerk is hospital-exclusive", []string{"records_clerk"}, true},
		{"admin is shared with every vertical, not trusted alone", []string{"admin"}, false},
		{"superuser is shared with every vertical, not trusted alone", []string{"superuser"}, false},
		{"manager is shared with every vertical, not trusted alone", []string{"manager"}, false},
		{"cashier is shared with POS/hospitality, not trusted alone", []string{"cashier"}, false},
		{"receptionist is shared with hospitality/services, not trusted alone", []string{"receptionist"}, false},
		{"one unambiguous role among several is enough", []string{"manager", "nurse"}, true},
		{"no roles", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasUnambiguousHospitalRole(tc.roles); got != tc.want {
				t.Errorf("HasUnambiguousHospitalRole(%v) = %v, want %v", tc.roles, got, tc.want)
			}
		})
	}
}
