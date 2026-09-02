package consultation

import (
	"testing"

	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/examinationrecord"
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
	"github.com/bengobox/hospital-service/internal/ent/schema"
)

func TestAppendDiagnosisHistory(t *testing.T) {
	clinician := uuid.New()

	t.Run("first record with no diagnosis yet appends nothing", func(t *testing.T) {
		got := appendDiagnosisHistory(nil, "", "", clinician)
		if len(got) != 0 {
			t.Errorf("expected no history entries, got %d", len(got))
		}
	})

	t.Run("first record WITH a diagnosis appends one entry", func(t *testing.T) {
		got := appendDiagnosisHistory(nil, "J00", "Common cold", clinician)
		if len(got) != 1 || got[0].Code != "J00" || got[0].ChangedBy != clinician {
			t.Errorf("expected one entry for J00/%s, got %+v", clinician, got)
		}
	})

	t.Run("unchanged diagnosis from prior record does not duplicate", func(t *testing.T) {
		prior := &ent.ExaminationRecord{
			DiagnosisCode: "J00", DiagnosisName: "Common cold",
			DiagnosisHistory: []schema.DiagnosisHistoryEntry{{Code: "J00", Name: "Common cold", ChangedBy: clinician}},
		}
		got := appendDiagnosisHistory(prior, "J00", "Common cold", clinician)
		if len(got) != 1 {
			t.Errorf("expected history to stay at 1 entry (no duplicate), got %d", len(got))
		}
	})

	t.Run("changed diagnosis carries forward prior history and appends the new value", func(t *testing.T) {
		prior := &ent.ExaminationRecord{
			DiagnosisCode: "J00", DiagnosisName: "Common cold",
			DiagnosisHistory: []schema.DiagnosisHistoryEntry{{Code: "J00", Name: "Common cold"}},
		}
		got := appendDiagnosisHistory(prior, "J45", "Asthma", clinician)
		if len(got) != 2 {
			t.Fatalf("expected 2 entries (carried forward + new), got %d", len(got))
		}
		if got[0].Code != "J00" || got[1].Code != "J45" || got[1].ChangedBy != clinician {
			t.Errorf("expected trail [J00, J45] with the new entry attributed to the clinician, got %+v", got)
		}
	})

	t.Run("clearing a diagnosis on this write leaves the trail as inherited", func(t *testing.T) {
		prior := &ent.ExaminationRecord{
			DiagnosisCode: "J00", DiagnosisName: "Common cold",
			DiagnosisHistory: []schema.DiagnosisHistoryEntry{{Code: "J00", Name: "Common cold"}},
		}
		got := appendDiagnosisHistory(prior, "", "", clinician)
		if len(got) != 1 || got[0].Code != "J00" {
			t.Errorf("expected the inherited single entry unchanged, got %+v", got)
		}
	})
}

func TestResolveQueueType(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want examinationrecord.QueueType
	}{
		{"empty string defaults to doctor", "", examinationrecord.QueueTypeDoctor},
		{"doctor passes through", "doctor", examinationrecord.QueueTypeDoctor},
		{"dental passes through", "dental", examinationrecord.QueueTypeDental},
		{"mch passes through", "mch", examinationrecord.QueueTypeMch},
		{"specialist passes through", "specialist", examinationrecord.QueueTypeSpecialist},
		{"unrecognised non-empty value passes through unvalidated (rejected later at Save)", "garbage", examinationrecord.QueueType("garbage")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveQueueType(tc.in); got != tc.want {
				t.Errorf("resolveQueueType(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNextVisitStatusAfterExamination(t *testing.T) {
	cases := []struct {
		name     string
		current  patientvisit.Status
		complete bool
		wantNext patientvisit.Status
		wantOK   bool
	}{
		{"completing from registered advances to completed", patientvisit.StatusRegistered, true, patientvisit.StatusCompleted, true},
		{"completing from triaged advances to completed", patientvisit.StatusTriaged, true, patientvisit.StatusCompleted, true},
		{"completing from in_examination advances to completed", patientvisit.StatusInExamination, true, patientvisit.StatusCompleted, true},
		{"completing an already-completed visit is a no-op", patientvisit.StatusCompleted, true, "", false},
		{"saving an in-progress note from triaged advances to in_examination", patientvisit.StatusTriaged, false, patientvisit.StatusInExamination, true},
		{"saving an in-progress note from registered does not advance (not yet triaged)", patientvisit.StatusRegistered, false, "", false},
		{"saving an in-progress note while already in_examination is a no-op", patientvisit.StatusInExamination, false, "", false},
		{"saving an in-progress note on a completed visit does not reopen it", patientvisit.StatusCompleted, false, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotNext, gotOK := nextVisitStatusAfterExamination(tc.current, tc.complete)
			if gotOK != tc.wantOK || gotNext != tc.wantNext {
				t.Errorf("nextVisitStatusAfterExamination(%q, %v) = (%q, %v), want (%q, %v)",
					tc.current, tc.complete, gotNext, gotOK, tc.wantNext, tc.wantOK)
			}
		})
	}
}

func TestNextVisitStatusAfterReferral(t *testing.T) {
	cases := []struct {
		name       string
		referredTo string
		wantNext   patientvisit.Status
		wantOK     bool
	}{
		{"lab referral advances to awaiting_lab", "lab", patientvisit.StatusAwaitingLab, true},
		{"pharmacy referral advances to prescribed", "pharmacy", patientvisit.StatusPrescribed, true},
		{"external_facility referral has no status transition", "external_facility", "", false},
		{"specialist referral has no status transition", "specialist", "", false},
		{"empty destination has no status transition", "", "", false},
		{"unrecognised destination has no status transition", "garbage", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotNext, gotOK := nextVisitStatusAfterReferral(tc.referredTo)
			if gotOK != tc.wantOK || gotNext != tc.wantNext {
				t.Errorf("nextVisitStatusAfterReferral(%q) = (%q, %v), want (%q, %v)",
					tc.referredTo, gotNext, gotOK, tc.wantNext, tc.wantOK)
			}
		})
	}
}

// NOTE: The diagnosis snapshot on ExaminationRecord (diagnosis_code/diagnosis_name are a
// snapshot at exam time, not a live FK to DiagnosisCatalogEntry/DiagnosisCatalogDefault — see
// the migration plan doc) has no hidden pure "build snapshot from catalog entry" function to
// extract here: RecordExamination takes req.DiagnosisCode/req.DiagnosisName as opaque strings
// straight from the caller and stores them verbatim: there's no catalog lookup-and-snapshot step
// in this service to pull out. ListDiagnosisCatalog's global+tenant union and
// CreateDiagnosisEntry's single required-field check are similarly thin wrappers around ent
// queries with no non-trivial decision logic worth a pure kernel.
//
// The rest of RecordExamination/CreateReferral (ent create/update, transaction commit/rollback)
// and all of GetExamination/ListDiagnosisCatalog/CreateDiagnosisEntry/ListReferrals (plain ent
// queries) are inseparable from the ent client and are not covered by an automated test in this
// pass — hospital-api has no sqlite/in-memory ent driver wired for DB-free service tests (unlike
// e.g. pos-api's promotions package, which extracts a pure decision function specifically to
// avoid this). A follow-up should either add an ent sqlite test driver or an integration test
// against the local Postgres dev DB. chargeConsultationFee (same 2026-08-29 fix as patients'
// chargeRegistrationFee) has no separable pure kernel of its own — the once-per-visit decision is
// the priorExamCount==0 check already inline in RecordExamination above, and the catalog
// lookup/PostCharge call needs the ent client — same limitation, not a new gap.
