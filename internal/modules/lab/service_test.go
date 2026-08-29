package lab

import (
	"testing"

	"github.com/bengobox/hospital-service/internal/ent/examinationrecord"
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
)

func TestNextVisitStatusAfterLabComplete(t *testing.T) {
	cases := []struct {
		name     string
		current  patientvisit.Status
		wantNext patientvisit.Status
		wantOK   bool
	}{
		{"awaiting_lab advances to lab_complete", patientvisit.StatusAwaitingLab, patientvisit.StatusLabComplete, true},
		{"registered is left untouched (never referred to lab)", patientvisit.StatusRegistered, "", false},
		{"triaged is left untouched", patientvisit.StatusTriaged, "", false},
		{"in_examination is left untouched", patientvisit.StatusInExamination, "", false},
		{"already lab_complete is not re-advanced (a later order's last line resulting doesn't repeat the transition)", patientvisit.StatusLabComplete, "", false},
		{"prescribed is left untouched", patientvisit.StatusPrescribed, "", false},
		{"dispensed is left untouched", patientvisit.StatusDispensed, "", false},
		{"admitted is left untouched", patientvisit.StatusAdmitted, "", false},
		{"completed is never rewound by a late-resulting order", patientvisit.StatusCompleted, "", false},
		{"cancelled is never resurrected by a late-resulting order", patientvisit.StatusCancelled, "", false},
		{"zero-value status (unexpected/empty) does not advance", patientvisit.Status(""), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotNext, gotOK := nextVisitStatusAfterLabComplete(tc.current)
			if gotOK != tc.wantOK || gotNext != tc.wantNext {
				t.Errorf("nextVisitStatusAfterLabComplete(%q) = (%q, %v), want (%q, %v)",
					tc.current, gotNext, gotOK, tc.wantNext, tc.wantOK)
			}
		})
	}
}

func TestNextExaminationStatusAfterLabComplete(t *testing.T) {
	cases := []struct {
		name     string
		current  examinationrecord.Status
		wantNext examinationrecord.Status
		wantOK   bool
	}{
		{"awaiting_lab reopens to in_progress", examinationrecord.StatusAwaitingLab, examinationrecord.StatusInProgress, true},
		{"completed reopens to in_progress (finalized note, confirmatory labs ordered after)", examinationrecord.StatusCompleted, examinationrecord.StatusInProgress, true},
		{"already in_progress is a no-op (no redundant write)", examinationrecord.StatusInProgress, "", false},
		{"zero-value status (unexpected/empty) does not reopen", examinationrecord.Status(""), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotNext, gotOK := nextExaminationStatusAfterLabComplete(tc.current)
			if gotOK != tc.wantOK || gotNext != tc.wantNext {
				t.Errorf("nextExaminationStatusAfterLabComplete(%q) = (%q, %v), want (%q, %v)",
					tc.current, gotNext, gotOK, tc.wantNext, tc.wantOK)
			}
		})
	}
}

// NOTE: The rest of EnterResult (ent create/update, the all-lines-resulted scan, transaction
// commit/rollback, outbox publish) and all of CreateOrder/GetOrder/ListWorklist/ActivateIfPaid/
// SubmitInsuranceClaim/ListCatalog (plain ent queries/mutations) are inseparable from the ent
// client and are not covered by an automated test in this pass — hospital-api has no
// sqlite/in-memory ent driver wired for DB-free service tests, same limitation noted in
// consultation/service_test.go and patients/service_test.go.
