package config

import (
	"log"
	"os"
	"strings"
	"time"
)

const (
	DefaultPort           = "28080"
	DefaultRoot           = "/media"
	MaxUploadBytes        = int64(8 << 30)
	MaxMemoryBytes        = int64(32 << 20)
	MountTimeout          = 30 * time.Second
	UmountTimeout         = 30 * time.Second
	DefaultRequestTimeout = 10 * time.Minute
)

var RequestTimeout = DurationFromEnv("REQUEST_TIMEOUT", DefaultRequestTimeout)

func Getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func DurationFromEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		log.Printf("invalid %s=%q; fallback to %s", key, value, fallback)
		return fallback
	}
	return duration
}
