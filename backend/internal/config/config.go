package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port           string
	DatabaseURL    string
	AllowedOrigins []string
	CycleMs        int64
	SyncLeadMs     int64
	SeedOnStart    bool
	MinSyncMs      int64
	MaxSyncMs      int64
}

func Load() (Config, error) {
	c := Config{
		Port:        env("PORT", "8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/sequencer?sslmode=disable"),
		MinSyncMs:   1_000,
		MaxSyncMs:   3_600_000,
	}

	c.AllowedOrigins = splitCSV(env("ALLOWED_ORIGINS", "http://localhost:5173"))

	var err error
	if c.CycleMs, err = envInt("CYCLE_MS", 18_000_000); err != nil {
		return c, err
	}
	if c.CycleMs <= 0 {
		return c, fmt.Errorf("CYCLE_MS must be > 0")
	}
	if c.SyncLeadMs, err = envInt("SYNC_LEAD_MS", 1_500); err != nil {
		return c, err
	}
	if c.SyncLeadMs < 0 {
		return c, fmt.Errorf("SYNC_LEAD_MS must be >= 0")
	}
	c.SeedOnStart = strings.EqualFold(env("SEED_ON_START", "true"), "true")

	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int64) (int64, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
