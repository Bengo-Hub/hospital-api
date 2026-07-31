package handlers

import "net/http"

// Ping is a placeholder authenticated route proving the JWKS auth middleware
// is wired correctly. Sprint 0 replaces this with real hospital-api routes
// (patients, visits, triage, ...).
func Ping(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "hospital-api"})
}
