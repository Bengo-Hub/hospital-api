package patients

import (
	"testing"
	"time"

	"github.com/bengobox/hospital-service/internal/ent"
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

func TestAllergyFlagsEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both empty", nil, []string{}, true},
		{"identical order", []string{"penicillin", "latex"}, []string{"penicillin", "latex"}, true},
		{"same set, different order", []string{"penicillin", "latex"}, []string{"latex", "penicillin"}, true},
		{"a genuinely new allergy added", []string{"penicillin"}, []string{"penicillin", "latex"}, false},
		{"an allergy removed", []string{"penicillin", "latex"}, []string{"penicillin"}, false},
		{"different length, same total count (dup collapses)", []string{"penicillin", "penicillin"}, []string{"penicillin"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := allergyFlagsEqual(tc.a, tc.b); got != tc.want {
				t.Errorf("allergyFlagsEqual(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestAcuityRank(t *testing.T) {
	cases := []struct {
		name     string
		priority string
		want     int
	}{
		{"ESI 1 is most urgent", "1", 0},
		{"emergency word-scale matches ESI 1", "emergency", 0},
		{"Emergency is case-insensitive", "Emergency", 0},
		{"ESI 2", "2", 1},
		{"urgent word-scale matches ESI 2", "urgent", 1},
		{"ESI 5 is least urgent", "5", 4},
		{"routine word-scale matches ESI 5", "routine", 4},
		{"unrecognised value ranks last", "garbage", 99},
		{"empty (not yet triaged) ranks last", "", 99},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acuityRank(tc.priority); got != tc.want {
				t.Errorf("acuityRank(%q) = %d, want %d", tc.priority, got, tc.want)
			}
		})
	}
}

func TestSortVisitsByAcuity(t *testing.T) {
	mk := func(status patientvisit.Status, priority string) *ent.PatientVisit {
		v := &ent.PatientVisit{Status: status}
		if priority != "" {
			v.Edges.TriageRecords = []*ent.TriageRecord{{Priority: priority, TakenAt: time.Now()}}
		}
		return v
	}

	t.Run("urgent-first within the registered/triaged bucket, FIFO otherwise", func(t *testing.T) {
		routine := mk(patientvisit.StatusTriaged, "routine")
		untriaged := mk(patientvisit.StatusRegistered, "")
		emergency := mk(patientvisit.StatusTriaged, "emergency")
		visits := []*ent.PatientVisit{routine, untriaged, emergency}

		sortVisitsByAcuity(visits)

		if visits[0] != emergency {
			t.Errorf("expected emergency visit first, got status=%s", visits[0].Status)
		}
		if visits[1] != routine {
			t.Errorf("expected routine (triaged, lower acuity) second, got status=%s", visits[1].Status)
		}
		if visits[2] != untriaged {
			t.Errorf("expected untriaged (no data, ranks last) third, got status=%s", visits[2].Status)
		}
	})

	t.Run("visits past triage keep their original slot, never reordered", func(t *testing.T) {
		emergency := mk(patientvisit.StatusTriaged, "emergency")
		inExam := mk(patientvisit.StatusInExamination, "")
		routine := mk(patientvisit.StatusTriaged, "routine")
		visits := []*ent.PatientVisit{routine, inExam, emergency}

		sortVisitsByAcuity(visits)

		if visits[1] != inExam {
			t.Errorf("in_examination visit must stay in its original slot 1, got status=%s", visits[1].Status)
		}
		if visits[0] != emergency || visits[2] != routine {
			t.Errorf("registered/triaged bucket (slots 0,2) should be urgent-first: got %s, %s", visits[0].Status, visits[2].Status)
		}
	})

	t.Run("fewer than 2 reorderable visits is a no-op", func(t *testing.T) {
		inExam := mk(patientvisit.StatusInExamination, "")
		emergency := mk(patientvisit.StatusTriaged, "emergency")
		visits := []*ent.PatientVisit{inExam, emergency}

		sortVisitsByAcuity(visits)

		if visits[0] != inExam || visits[1] != emergency {
			t.Error("order must be unchanged when only one visit is in the reorderable bucket")
		}
	})
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
