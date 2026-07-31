// Command migrate applies versioned Atlas/ent migrations for hospital-api.
//
// This is a placeholder: there are no ent schemas yet (see internal/ent/schema/,
// currently empty). Sprint 0 adds the first schemas, generates the ent client
// (`go generate ./internal/ent/...`), and this binary starts applying
// internal/ent/migrate/migrations/*.sql the same way inventory-api/library-api do.
package main

import "log"

func main() {
	log.Println("hospital-api migrate: no ent schemas defined yet (Sprint 0 scaffold) — nothing to migrate")
}
