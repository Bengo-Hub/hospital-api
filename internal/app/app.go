package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	authclient "github.com/Bengo-Hub/shared-auth-client"

	"github.com/bengobox/hospital-service/internal/config"
	handlers "github.com/bengobox/hospital-service/internal/http/handlers"
	router "github.com/bengobox/hospital-service/internal/http/router"
	"github.com/bengobox/hospital-service/internal/platform/cache"
	"github.com/bengobox/hospital-service/internal/platform/database"
	"github.com/bengobox/hospital-service/internal/platform/events"
	"github.com/bengobox/hospital-service/internal/shared/logger"
)

// App holds the wired runtime for the hospital service.
//
// This is a Sprint-0 scaffold: config/logging/db-pool/redis/nats/health/router
// are wired for real, but there is no ent client, no domain schema, and no
// outbox publisher yet (those need at least one ent schema to exist). Sprint 0
// adds the first ent schemas (Patient, PatientVisit, ...) and wires them here
// the same way library-api/inventory-api wire theirs.
type App struct {
	cfg        *config.Config
	log        *zap.Logger
	httpServer *http.Server
	db         *pgxpool.Pool
	cache      *redis.Client
	events     *nats.Conn
}

// New constructs and wires the application.
func New(ctx context.Context) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	log, err := logger.New(cfg.App.Env)
	if err != nil {
		return nil, fmt.Errorf("logger init: %w", err)
	}

	dbPool, err := database.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return nil, fmt.Errorf("postgres init: %w", err)
	}
	redisClient := cache.NewClient(cfg.Redis)

	natsConn, err := events.Connect(cfg.Events)
	if err != nil {
		log.Warn("event bus connection failed", zap.Error(err))
	}
	if natsConn != nil {
		if streamErr := events.EnsureStream(natsConn, cfg.Events); streamErr != nil {
			log.Warn("failed to ensure hospital stream", zap.Error(streamErr))
		}
	}

	healthHandler := handlers.NewHealthHandler(log, dbPool, redisClient, natsConn)

	// auth-service JWT validator (JWKS) + optional S2S API key.
	authConfig := authclient.DefaultConfig(cfg.Auth.JWKSUrl, cfg.Auth.Issuer, cfg.Auth.Audience)
	authConfig.CacheTTL = cfg.Auth.JWKSCacheTTL
	authConfig.RefreshInterval = cfg.Auth.JWKSRefreshInterval
	validator, err := authclient.NewValidator(authConfig)
	if err != nil {
		return nil, fmt.Errorf("auth validator init: %w", err)
	}

	var authMiddleware *authclient.AuthMiddleware
	if cfg.Auth.EnableAPIKeyAuth {
		apiKeyValidator := authclient.NewAPIKeyValidator(cfg.Auth.ServiceURL, nil)
		authMiddleware = authclient.NewAuthMiddlewareWithAPIKey(validator, apiKeyValidator)
	} else {
		authMiddleware = authclient.NewAuthMiddleware(validator)
	}

	deps := router.Deps{
		Log:            log,
		Health:         healthHandler,
		AuthMiddleware: authMiddleware,
		AllowedOrigins: cfg.HTTP.AllowedOrigins,
	}
	chiRouter := router.New(deps)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port),
		Handler:           chiRouter,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	return &App{
		cfg:        cfg,
		log:        log,
		httpServer: httpServer,
		db:         dbPool,
		cache:      redisClient,
		events:     natsConn,
	}, nil
}

// Run starts the HTTP server and blocks until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	if a.cfg.HTTP.TLSCertFile != "" && a.cfg.HTTP.TLSKeyFile != "" {
		a.log.Info("hospital service starting with HTTPS", zap.String("addr", a.httpServer.Addr))
		go func() { errCh <- a.httpServer.ListenAndServeTLS(a.cfg.HTTP.TLSCertFile, a.cfg.HTTP.TLSKeyFile) }()
	} else {
		a.log.Info("hospital service starting with HTTP", zap.String("addr", a.httpServer.Addr))
		go func() { errCh <- a.httpServer.ListenAndServe() }()
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server error: %w", err)
	}
}

// Close releases resources in reverse dependency order.
func (a *App) Close() {
	if a.events != nil {
		_ = a.events.Drain()
		a.events.Close()
	}
	if a.cache != nil {
		_ = a.cache.Close()
	}
	if a.db != nil {
		a.db.Close()
	}
	_ = a.log.Sync()
}
