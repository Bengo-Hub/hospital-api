// Package refdata seeds global (nil-tenant) reference data — currently the starter ICD-11
// diagnosis catalogue. Mirrors library-api's refdata.SeedGlobal* pattern
// (feedback_shared_core_reference_data.md: reference/catalog data is global, never
// tenant-scoped) rather than ad-hoc seed code scattered per module.
package refdata

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/diagnosiscatalogdefault"
)

type diagnosisSeed struct {
	code     string
	name     string
	category string
}

// starterDiagnoses is a SMALL starter set of common presenting diagnoses for a Kenyan primary/
// outpatient care setting, coded ICD-11 (per the confirmed Taifa Care claim contract — see
// docs/integrations.md §2.4). This is deliberately NOT a full ICD-11 import — that is a
// separate, much larger data-loading effort (the WHO's full ICD-11 release) — flagged here so a
// future session doesn't mistake this starter list for complete clinical coverage.
var starterDiagnoses = []diagnosisSeed{
	{"1A00", "Cholera", "Infectious"},
	{"1A07", "Typhoid fever", "Infectious"},
	{"1F43", "Malaria", "Infectious"},
	{"1C62", "Urinary tract infection, site not specified", "Infectious"},
	{"CA07", "Acute upper respiratory infection", "Respiratory"},
	{"CA40", "Pneumonia, organism unspecified", "Respiratory"},
	{"DB90", "Acute gastroenteritis", "Digestive"},
	{"BA00", "Essential hypertension", "Cardiovascular"},
	{"5A11", "Type 2 diabetes mellitus", "Endocrine"},
	{"JA00", "Pregnancy, confirmed", "Maternal/Obstetric"},
	{"JB00", "Normal delivery", "Maternal/Obstetric"},
	{"NA00", "Superficial injury of head", "Injury"},
	{"NE60", "Burn of unspecified body region", "Injury"},
	{"EB00", "Contact dermatitis", "Dermatological"},
	{"8A80", "Migraine", "Neurological"},
	{"FA00", "Osteoarthritis", "Musculoskeletal"},
	{"6A70", "Depressive disorder", "Mental Health"},
	{"6B00", "Anxiety disorder", "Mental Health"},
	{"1E51", "HIV disease", "Infectious"},
	{"1B10", "Tuberculosis of lung", "Infectious"},
}

// SeedGlobalDiagnosisCatalog idempotently upserts the starter diagnosis catalogue. Safe to run
// on every deploy — matches on the unique `code` index and skips rows that already exist.
func SeedGlobalDiagnosisCatalog(ctx context.Context, client *ent.Client, log *zap.Logger) error {
	log = log.Named("refdata")
	created := 0
	for _, d := range starterDiagnoses {
		exists, err := client.DiagnosisCatalogDefault.Query().
			Where(diagnosiscatalogdefault.Code(d.code)).
			Exist(ctx)
		if err != nil {
			return fmt.Errorf("refdata: check diagnosis %s: %w", d.code, err)
		}
		if exists {
			continue
		}
		if _, err := client.DiagnosisCatalogDefault.Create().
			SetCode(d.code).SetName(d.name).SetCategory(d.category).
			Save(ctx); err != nil {
			return fmt.Errorf("refdata: create diagnosis %s: %w", d.code, err)
		}
		created++
	}
	log.Info("global diagnosis catalog seeded", zap.Int("created", created), zap.Int("total", len(starterDiagnoses)))
	return nil
}
