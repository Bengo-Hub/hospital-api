package config

import (
	"fmt"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

const namespace = ""

// Config aggregates runtime configuration for the hospital service.
type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	Postgres PostgresConfig
	Redis    RedisConfig
	Events   EventsConfig
	Auth     AuthConfig
	Services ServicesConfig
	Media    MediaConfig
}

// MediaConfig mirrors inventory-api's own MediaConfig (internal/config/config.go there) — same
// upload-root/URL-base shape, reused here for Patient.photo_url uploads.
type MediaConfig struct {
	Root    string `envconfig:"MEDIA_ROOT" default:"./media"`
	URLBase string `envconfig:"MEDIA_URL_BASE" default:""`
}

type AppConfig struct {
	Name    string `envconfig:"APP_NAME" default:"hospital-service"`
	Env     string `envconfig:"APP_ENV" default:"development"`
	Version string `envconfig:"APP_VERSION" default:"0.1.0"`
}

type HTTPConfig struct {
	Host           string        `envconfig:"HTTP_HOST" default:"0.0.0.0"`
	Port           int           `envconfig:"HTTP_PORT" default:"4200"`
	ReadTimeout    time.Duration `envconfig:"HTTP_READ_TIMEOUT" default:"20s"`
	WriteTimeout   time.Duration `envconfig:"HTTP_WRITE_TIMEOUT" default:"20s"`
	IdleTimeout    time.Duration `envconfig:"HTTP_IDLE_TIMEOUT" default:"90s"`
	TLSCertFile    string        `envconfig:"TLS_CERT_FILE"`
	TLSKeyFile     string        `envconfig:"TLS_KEY_FILE"`
	AllowedOrigins []string      `envconfig:"HTTP_ALLOWED_ORIGINS" default:"https://afya.codevertexafrica.com,https://accounts.codevertexafrica.com"`
}

type PostgresConfig struct {
	URL                      string        `envconfig:"POSTGRES_URL" default:"postgres://postgres:postgres@localhost:5432/hospital?sslmode=disable"`
	MigrateURL               string        `envconfig:"POSTGRES_MIGRATE_URL" default:""`
	MaxOpenConns             int           `envconfig:"POSTGRES_MAX_OPEN_CONNS" default:"5"`
	MaxIdleConns             int           `envconfig:"POSTGRES_MAX_IDLE_CONNS" default:"3"`
	ConnMaxLifetime          time.Duration `envconfig:"POSTGRES_CONN_MAX_LIFETIME" default:"5m"`
	StatementTimeout         time.Duration `envconfig:"POSTGRES_STATEMENT_TIMEOUT" default:"30s"`
	IdleInTransactionTimeout time.Duration `envconfig:"POSTGRES_IDLE_IN_TRANSACTION_TIMEOUT" default:"60s"`
	RunMigrations            bool          `envconfig:"POSTGRES_RUN_MIGRATIONS" default:"false"`
}

type RedisConfig struct {
	Addr        string        `envconfig:"REDIS_ADDR" default:"localhost:6380"`
	Username    string        `envconfig:"REDIS_USERNAME"`
	Password    string        `envconfig:"REDIS_PASSWORD"`
	DB          int           `envconfig:"REDIS_DB" default:"0"`
	TLSRequired bool          `envconfig:"REDIS_TLS_REQUIRED" default:"false"`
	DialTimeout time.Duration `envconfig:"REDIS_DIAL_TIMEOUT" default:"5s"`
}

// EventsConfig wires the hospital NATS JetStream stream. Subjects follow
// {aggregate_type}.{event_type} with aggregate_type always "hospital"
// (see shared-docs/event-architecture.md).
type EventsConfig struct {
	NATSURL      string `envconfig:"EVENTS_NATS_URL" default:"nats://localhost:4222"`
	StreamName   string `envconfig:"NATS_STREAM" default:"hospital"`
	DeliverGroup string `envconfig:"NATS_DELIVER_GROUP" default:"hospital-workers"`

	// Transactional outbox poller (drains internal/ent/schema/outbox_event.go rows to NATS).
	OutboxEnabled    bool          `envconfig:"EVENTS_OUTBOX_ENABLED" default:"true"`
	OutboxBatchSize  int           `envconfig:"EVENTS_OUTBOX_BATCH_SIZE" default:"50"`
	OutboxPollPeriod time.Duration `envconfig:"EVENTS_OUTBOX_POLL_PERIOD" default:"2s"`
}

// AuthConfig validates SSO JWTs via auth-api's JWKS endpoint (Trinity Layer 1).
type AuthConfig struct {
	ServiceURL          string        `envconfig:"AUTH_SERVICE_URL" default:"https://sso.codevertexafrica.com"`
	Issuer              string        `envconfig:"AUTH_ISSUER" default:"https://sso.codevertexafrica.com"`
	Audience            string        `envconfig:"AUTH_AUDIENCE" default:"codevertex"`
	JWKSUrl             string        `envconfig:"AUTH_JWKS_URL" default:"https://sso.codevertexafrica.com/api/v1/.well-known/jwks.json"`
	JWKSCacheTTL        time.Duration `envconfig:"AUTH_JWKS_CACHE_TTL" default:"3600s"`
	JWKSRefreshInterval time.Duration `envconfig:"AUTH_JWKS_REFRESH_INTERVAL" default:"300s"`
	EnableAPIKeyAuth    bool          `envconfig:"AUTH_ENABLE_API_KEY_AUTH" default:"true"`
	APIKey              string        `envconfig:"INTERNAL_SERVICE_KEY" default:""`
	// WitnessTokenSecret signs the short-lived (~2 minute) internal HS256 token minted by
	// POST .../pharmacy/verify-witness after a controlled-substance dispense witness
	// re-authenticates their own email+password against auth-api — see
	// internal/modules/pharmacy/witness.go. No JWT/HMAC signing secret previously existed in
	// this service (unlike pos-api's TERMINAL_JWT_SECRET); falls back to APIKey
	// (INTERNAL_SERVICE_KEY) only to avoid a hard startup failure in dev/local environments —
	// set this explicitly in production, mirroring pos-api's own fallback pattern.
	WitnessTokenSecret string `envconfig:"PHARMACY_WITNESS_JWT_SECRET" default:""`
}

// ServicesConfig holds the S2S base URLs for the services hospital-api calls
// by reference instead of duplicating their data (see docs/integrations.md).
type ServicesConfig struct {
	InventoryURL     string `envconfig:"INVENTORY_SERVICE_URL" default:"https://inventoryapi.codevertexafrica.com"`
	TreasuryURL      string `envconfig:"TREASURY_SERVICE_URL" default:"https://booksapi.codevertexafrica.com"`
	NotificationsURL string `envconfig:"NOTIFICATIONS_SERVICE_URL" default:"https://notificationsapi.codevertexafrica.com"`
	SubscriptionsURL string `envconfig:"SUBSCRIPTION_BASE_URL" default:"https://pricingapi.codevertexafrica.com"`
}

// Load gathers configuration from environment variables and optional .env files.
func Load() (*Config, error) {
	_ = godotenv.Load()

	var cfg Config
	if err := envconfig.Process(namespace, &cfg); err != nil {
		return nil, fmt.Errorf("config: failed to load environment variables: %w", err)
	}

	return &cfg, nil
}
