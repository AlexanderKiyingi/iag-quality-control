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

// StrictRBAC enables fail-closed permission checks in production. In
// non-production environments missing claims fall through (open) to keep
// local dev and integration tests workable.
func (c Config) StrictRBAC() bool {
	return c.IsProduction()
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
