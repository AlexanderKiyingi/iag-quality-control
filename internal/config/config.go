package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Environment string
	Port          string
	ServiceName   string
	DatabaseURL   string
	AutoMigrate   bool

	AuthMode            string
	JWTIssuer           string
	JWKSURL             string
	Audience            string
	ServiceClientID     string
	ServiceClientSecret string
	AuthTokenURL        string

	KafkaBrokers  []string
	KafkaTopic    string
	KafkaClientID string
	KafkaRequired bool

	UpstreamSCM              string
	UpstreamMES              string
	AutoValidateBatchSCM     bool
	AutoValidateExportLotSCM bool
	InstrumentSyncInterval   time.Duration
}

func Load() (Config, error) {
	_ = godotenv.Load()
	env := strings.ToLower(strings.TrimSpace(getenv("ENVIRONMENT", "development")))
	authMode := strings.ToLower(strings.TrimSpace(getenv("AUTH_MODE", "jwt")))
	if authMode != "jwt" {
		return Config{}, fmt.Errorf("AUTH_MODE must be jwt (got %q)", authMode)
	}
	c := Config{
		Environment:   env,
		Port:          getenv("PORT", "4004"),
		ServiceName:   getenv("SERVICE_NAME", "quality-control"),
		DatabaseURL:   strings.TrimSpace(os.Getenv("DATABASE_URL")),
		AutoMigrate:   getenv("AUTO_MIGRATE", "true") != "false",
		AuthMode:      authMode,
		JWTIssuer:     getenv("JWT_ISSUER", "http://localhost:3001"),
		JWKSURL:       getenv("JWKS_URL", "http://localhost:3001/.well-known/jwks.json"),
		Audience:      getenv("AUDIENCE", "iag.quality"),
		ServiceClientID:     getenv("SERVICE_CLIENT_ID", "iag-quality-control"),
		ServiceClientSecret: os.Getenv("SERVICE_CLIENT_SECRET"),
		KafkaBrokers:  splitCSV(getenv("KAFKA_BROKERS", "")),
		KafkaTopic:    getenv("KAFKA_QUALITY_TOPIC", "iag.quality"),
		KafkaClientID: getenv("KAFKA_CLIENT_ID", "iag-quality-control"),
		KafkaRequired: getenv("KAFKA_REQUIRED", "false") == "true",
		UpstreamSCM:              strings.TrimSpace(os.Getenv("UPSTREAM_SUPPLY_CHAIN")),
		UpstreamMES:                strings.TrimSpace(os.Getenv("UPSTREAM_MES")),
		AutoValidateBatchSCM:     strings.EqualFold(getenv("AUTO_VALIDATE_BATCH_SCM", "false"), "true"),
		AutoValidateExportLotSCM: strings.EqualFold(getenv("AUTO_VALIDATE_EXPORT_LOT_SCM", "false"), "true"),
		InstrumentSyncInterval:     parseDuration(getenv("INSTRUMENT_SYNC_INTERVAL", "15m"), 15*time.Minute),
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if c.AuthTokenURL == "" {
		c.AuthTokenURL = strings.TrimRight(c.JWTIssuer, "/") + "/oauth/token"
	}
	if c.IsProduction() {
		if c.ServiceClientSecret == "" {
			return c, fmt.Errorf("SERVICE_CLIENT_SECRET is required in production")
		}
		if len(c.ServiceClientSecret) < 16 {
			return c, fmt.Errorf("SERVICE_CLIENT_SECRET must be at least 16 characters in production")
		}
	}
	return c, nil
}

func (c Config) IsProduction() bool {
	return c.Environment == "production" || c.Environment == "prod"
}

// StrictRBAC denies access when a verified token carries no permissions
// (fail-closed).
func (c Config) StrictRBAC() bool { return c.HardenedRuntime() }

// HardenedRuntime reports whether production safeguards apply.
//
// It deliberately does not just return IsProduction(). That required
// ENVIRONMENT=production, which the Railway runbooks never told anyone to set,
// so a hosted instance fell back to the "development" default and ran
// fail-OPEN: the permission middleware grants EVERY permission to a token
// carrying an empty permissions array. An unset ENVIRONMENT on a deployed
// instance now hardens instead; only an explicit dev-like value opts out.
//
// This cannot prevent boot — the worst case is a 403 for a caller that should
// never have had access. Boot-time validation stays keyed on ENVIRONMENT alone.
//
// Mirrors iag-fleet's config.HardenedRuntime; the intent is one shared
// implementation in shared/platform-go once every service is on it.
func (c Config) HardenedRuntime() bool {
	// An explicit production value always hardens, including on a Config built
	// by hand in a test rather than through Load.
	if c.IsProduction() {
		return true
	}
	if environmentExplicitlySet() {
		return !c.isDevLike()
	}
	return deployedRuntime()
}

// isDevLike reports an environment where fail-open behaviour is a deliberate
// local convenience rather than an accident.
func (c Config) isDevLike() bool {
	switch c.Environment {
	case "development", "dev", "local", "test":
		return true
	}
	return false
}

// environmentExplicitlySet distinguishes a deliberately configured environment
// from the "development" value Load falls back to when nothing is set. Read
// from the process rather than captured on Config: StrictRBAC is resolved once
// during startup wiring, and the environment does not change under us.
func environmentExplicitlySet() bool {
	return strings.TrimSpace(os.Getenv("ENVIRONMENT")) != "" ||
		strings.TrimSpace(os.Getenv("APP_ENV")) != ""
}

// deployedRuntime distinguishes a hosted instance from a laptop: Railway's
// injected variables, or gin in release mode, which the Dockerfiles set.
func deployedRuntime() bool {
	if strings.TrimSpace(os.Getenv("RAILWAY_ENVIRONMENT")) != "" ||
		strings.TrimSpace(os.Getenv("RAILWAY_PROJECT_ID")) != "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GIN_MODE")), "release")
}

func getenv(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

func parseDuration(raw string, fallback time.Duration) time.Duration {
	if d, err := time.ParseDuration(strings.TrimSpace(raw)); err == nil && d > 0 {
		return d
	}
	return fallback
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
