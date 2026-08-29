package patients

import (
	"testing"

	"github.com/bengobox/hospital-service/internal/ent/billableitemcatalog"
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
)

func TestResolveVisitType(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want patientvisit.VisitType
	}{
		{"exact IPD selects inpatient", "IPD", patientvisit.VisitTypeIPD},
		{"exact OPD stays outpatient", "OPD", patientvisit.VisitTypeOPD},
		{"empty string defaults to OPD", "", patientvisit.VisitTypeOPD},
		{"lowercase ipd does not match, defaults to OPD", "ipd", patientvisit.VisitTypeOPD},
		{"unrecognised value defaults to OPD", "garbage", patientvisit.VisitTypeOPD},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveVisitType(tc.in); got != tc.want {
				t.Errorf("resolveVisitType(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNextVisitStatusAfterTriage(t *testing.T) {
	cases := []struct {
		name     string
		current  patientvisit.Status
		wantNext patientvisit.Status
		wantOK   bool
	}{
		{"registered advances to triaged", patientvisit.StatusRegistered, patientvisit.StatusTriaged, true},
		{"already triaged does not rewind or re-advance", patientvisit.StatusTriaged, "", false},
		{"in_examination is left untouched (re-triage mid-consult)", patientvisit.StatusInExamination, "", false},
		{"awaiting_lab is left untouched", patientvisit.StatusAwaitingLab, "", false},
		{"admitted is left untouched", patientvisit.StatusAdmitted, "", false},
		{"completed is never rewound by a re-triage", patientvisit.StatusCompleted, "", false},
		{"cancelled is never resurrected by a re-triage", patientvisit.StatusCancelled, "", false},
		{"zero-value status (unexpected/empty) does not advance", patientvisit.Status(""), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotNext, gotOK := nextVisitStatusAfterTriage(tc.current)
			if gotOK != tc.wantOK || gotNext != tc.wantNext {
				t.Errorf("nextVisitStatusAfterTriage(%q) = (%q, %v), want (%q, %v)",
					tc.current, gotNext, gotOK, tc.wantNext, tc.wantOK)
			}
		})
	}
}

func TestRegistrationAppliesTo(t *testing.T) {
	cases := []struct {
		name            string
		priorVisitCount int
		want            billableitemcatalog.AppliesTo
	}{
		{"first visit ever (0 prior) charges the first-visit fee", 0, billableitemcatalog.AppliesToFirstVisit},
		{"one prior visit charges the return-visit fee", 1, billableitemcatalog.AppliesToReturnVisit},
		{"many prior visits still charge the return-visit fee", 12, billableitemcatalog.AppliesToReturnVisit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := registrationAppliesTo(tc.priorVisitCount); got != tc.want {
				t.Errorf("registrationAppliesTo(%d) = %q, want %q", tc.priorVisitCount, got, tc.want)
			}
		})
	}
}

// NOTE: RegisterPatient's and CheckInVisit's own required-field checks (full_name / patient_id
// non-empty) are single-line guard clauses with no decision table worth extracting on their
// own — they're exercised indirectly by the pure functions above wherever they share a code
// path. chargeRegistrationFee/findActiveBillableItem's catalog lookup + PostCharge call are
// inseparable from the ent client (same as the rest of RegisterPatient/CheckInVisit/RecordTriage:
// sequence allocation, ent create/update, outbox event publish, transaction commit/rollback) and
// are not covered by an automated test in this pass — hospital-api has no sqlite/in-memory ent
// driver wired for DB-free service tests (unlike e.g. pos-api's promotions package, which
// extracts a pure decision function specifically to avoid this). A follow-up should either add
// an ent sqlite test driver or an integration test against the local Postgres dev DB.
