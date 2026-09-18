// Package config loads runtime configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port string

	DBPath string

	StorageBackend  string // "local" or "s3"
	LocalStorageDir string
	LocalURLSecret  string

	S3Bucket   string
	S3Region   string
	S3Endpoint string // optional, for S3-compatible endpoints

	JWKSURL      string
	JWKSCacheTTL time.Duration
	JWTAudience  string
	JWTIssuer    string

	WorkerCount int
}

func Load() Config {
	return Config{
		Port:            getEnv("PORT", "8080"),
		DBPath:          getEnv("DB_PATH", "data/assetvault.db"),
		StorageBackend:  getEnv("STORAGE_BACKEND", "local"),
		LocalStorageDir: getEnv("LOCAL_STORAGE_DIR", "data/objects"),
		LocalURLSecret:  getEnv("LOCAL_URL_SECRET", "dev-secret-change-me"),
		S3Bucket:        getEnv("S3_BUCKET", ""),
		S3Region:        getEnv("S3_REGION", "us-east-1"),
		S3Endpoint:      getEnv("S3_ENDPOINT", ""),
		JWKSURL:         getEnv("JWKS_URL", ""),
		JWKSCacheTTL:    getEnvDuration("JWKS_CACHE_TTL", 10*time.Minute),
		JWTAudience:     getEnv("JWT_AUDIENCE", ""),
		JWTIssuer:       getEnv("JWT_ISSUER", ""),
		WorkerCount:     getEnvInt("WORKER_COUNT", 2),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
