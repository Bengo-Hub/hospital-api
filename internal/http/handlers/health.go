package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type dbPinger interface {
	Ping(context.Context) error
}

// HealthHandler exposes liveness/readiness/metrics endpoints for hospital-api.
type HealthHandler struct {
	log    *zap.Logger
	db     dbPinger
	cache  *redis.Client
	events *nats.Conn
}

func NewHealthHandler(log *zap.Logger, db dbPinger, cache *redis.Client, events *nats.Conn) *HealthHandler {
	return &HealthHandler{log: log, db: db, cache: cache, events: events}
}

type livenessResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

type readinessResponse struct {
	Status       string            `json:"status"`
	Dependencies map[string]string `json:"dependencies"`
}

func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// Liveness reports whether the API process is running.
func (h *HealthHandler) Liveness(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, livenessResponse{Status: "ok", Service: "hospital-api"})
}

// Readiness checks downstream infrastructure dependencies.
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	issues := map[string]string{}
	if h.db != nil {
		if err := h.db.Ping(ctx); err != nil {
			issues["postgres"] = err.Error()
		}
	}
	if h.cache != nil {
		if err := h.cache.Ping(ctx).Err(); err != nil {
			issues["redis"] = err.Error()
		}
	}
	if h.events != nil && !h.events.IsConnected() {
		issues["nats"] = "not connected"
	}

	status := http.StatusOK
	if len(issues) > 0 {
		status = http.StatusServiceUnavailable
	}
	respondJSON(w, status, readinessResponse{Status: http.StatusText(status), Dependencies: issues})
}

// Metrics exposes Prometheus metrics.
func (h *HealthHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	promhttp.Handler().ServeHTTP(w, r)
}
