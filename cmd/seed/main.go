// Command seed loads idempotent reference/demo data for hospital-api.
//
// This is a placeholder until Sprint 0 lands the first ent schemas. Global
// reference data (e.g. a LabTest/DiagnosisCatalog default set) must be seeded
// under a nil-tenant sentinel, mirroring library-api's refdata.SeedGlobal*
// pattern (see feedback_shared_core_reference_data.md).
package main

import "log"

func main() {
	log.Println("hospital-api seed: no ent schemas defined yet (Sprint 0 scaffold) — nothing to seed")
}
