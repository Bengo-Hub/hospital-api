package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	authclient "github.com/Bengo-Hub/shared-auth-client"

	"github.com/bengobox/hospital-service/internal/config"
	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/migrate"
	handlers "github.com/bengobox/hospital-service/internal/http/handlers"
	router "github.com/bengobox/hospital-service/internal/http/router"
	"github.com/bengobox/hospital-service/internal/modules/identity"
	"github.com/bengobox/hospital-service/internal/modules/patients"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
	"github.com/bengobox/hospital-service/internal/modules/tenant"
	"github.com/bengobox/hospital-service/internal/platform/cache"
	"github.com/bengobox/hospital-service/internal/platform/database"
	"github.com/bengobox/hospital-service/internal/platform/events"
	"github.com/bengobox/hospital-service/internal/shared/logger"

	eventslib "github.com/Bengo-Hub/shared-events"
)

// App holds the wired runtime for the hospital service — Trinity Authorization plumbing
// (JWKS auth, RBAC, tenant/outlet sync, subscription gating, /auth/me) on top of the
// Sprint-0 scaffold. Clinical domain schemas (Patient, Prescription, LabOrder, ...) are
// deliberately NOT part of this: see docs/migration-pos-pharmacy.md Phase A / Sprint 4.
type App struct {
	cfg        *config.Config
	log        *zap.Logger
	httpServer *http.Server
	db         *pgxpool.Pool
	cache      *redis.Client
	events     *nats.Conn
	orm        *ent.Client

	authEventHandler       *identity.AuthEventHandler
	authOutletEventHandler *identity.AuthOutletEventHandler
	outboxPublisher        *eventslib.OutboxPoller
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

	// ── Ent ORM client (RBAC + tenant/outlet/user sync tables) ────────────────
	sqlDB, err := sql.Open("pgx", cfg.Postgres.URL)
	if err != nil {
		return nil, fmt.Errorf("ent driver init: %w", err)
	}
	sqlDB.SetMaxIdleConns(cfg.Postgres.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.Postgres.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(cfg.Postgres.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	drv := entsql.OpenDB(dialect.Postgres, sqlDB)
	ormClient := ent.NewClient(ent.Driver(drv))

	// Run versioned migrations only when explicitly enabled. In production, migrations are
	// applied by the entrypoint (or a migration Job) before the server starts.
	if cfg.Postgres.RunMigrations {
		if err := ormClient.Schema.Create(ctx, schema.WithDir(migrate.Dir)); err != nil {
			return nil, fmt.Errorf("ent schema create: %w", err)
		}
		log.Info("versioned migrations applied (POSTGRES_RUN_MIGRATIONS=true)")
	}

	// Transactional outbox poller (first real business events, Sprint 1: patient.created,
	// visit.admitted). Publishing = an outbox_events row inserted in the same Ent tx as the
	// domain write (internal/events.Publish); this poller drains PENDING rows to NATS.
	var outboxPublisher *eventslib.OutboxPoller
	if natsConn != nil && cfg.Events.OutboxEnabled {
		js, jsErr := natsConn.JetStream()
		if jsErr != nil {
			return nil, fmt.Errorf("jetstream init for outbox poller: %w", jsErr)
		}
		outboxRepo := eventslib.NewSQLOutboxRepository(sqlDB)
		outboxPublisher = eventslib.NewOutboxPoller(outboxRepo, eventslib.NewJetStreamAdapter(js, log), log, eventslib.PollerConfig{
			BatchSize:  cfg.Events.OutboxBatchSize,
			PollPeriod: cfg.Events.OutboxPollPeriod,
		})
		outboxPublisher.Start(ctx)
		log.Info("outbox background publisher started")
	}

	// ── Tenant/outlet sync + RBAC + JIT identity ──────────────────────────────
	tenantSyncer := tenant.NewSyncer(ormClient, cfg.Auth.ServiceURL).WithDB(sqlDB)

	rbacRepo := rbac.NewEntRepository(ormClient)
	rbacService := rbac.NewService(rbacRepo, log)
	if seedErr := rbacService.SeedRoles(ctx); seedErr != nil {
		log.Warn("rbac: seed global roles/permissions failed (will retry via JIT)", zap.Error(seedErr))
	}

	identitySvc := identity.NewService(ormClient, tenantSyncer)
	identitySvc.SetRBACService(rbacService)

	authMeHandler := handlers.NewAuthMeHandler(rbacService)

	// ── Sprint 1: patients / OPD reception / triage ───────────────────────────
	patientsSvc := patients.NewService(ormClient, log)
	patientsHandler := handlers.NewPatientsHandler(patientsSvc)

	authEventHandler := identity.NewAuthEventHandler(ormClient, identitySvc, log)
	authOutletEventHandler := identity.NewAuthOutletEventHandler(ormClient, tenantSyncer, log)

	// ── auth-service JWT validator (JWKS) + optional S2S API key ──────────────
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
		EntClient:      ormClient,
		IdentitySvc:    identitySvc,
		RBACSvc:        rbacService,
		AuthMe:         authMeHandler,
		Patients:       patientsHandler,
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
		cfg:                    cfg,
		log:                    log,
		httpServer:             httpServer,
		db:                     dbPool,
		cache:                  redisClient,
		events:                 natsConn,
		orm:                    ormClient,
		authEventHandler:       authEventHandler,
		authOutletEventHandler: authOutletEventHandler,
		outboxPublisher:        outboxPublisher,
	}, nil
}

// Run starts the HTTP server and blocks until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	if a.events != nil {
		if a.authEventHandler != nil {
			if err := a.authEventHandler.SubscribeToAuthEvents(a.events); err != nil {
				a.log.Warn("auth user event subscriptions not started", zap.Error(err))
			}
		}
		if a.authOutletEventHandler != nil {
			if err := a.authOutletEventHandler.SubscribeToOutletEvents(a.events); err != nil {
				a.log.Warn("auth outlet event subscriptions not started", zap.Error(err))
			}
		}
	}

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
	if a.outboxPublisher != nil {
		a.outboxPublisher.Stop()
	}
	if a.events != nil {
		_ = a.events.Drain()
		a.events.Close()
	}
	if a.cache != nil {
		_ = a.cache.Close()
	}
	if a.orm != nil {
		if err := a.orm.Close(); err != nil {
			a.log.Warn("ent client close failed", zap.Error(err))
		}
	}
	if a.db != nil {
		a.db.Close()
	}
	_ = a.log.Sync()
}
