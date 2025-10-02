// predictive-service/internal/config/config.go
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Redis struct {
		URL      string
		Password string
	}
	NodeAPI struct {
		BaseURL string
		Timeout time.Duration
	}
	Prediction struct {
		WindowSizeMinutes  int
		ConnectThreshold   float64
		ScaleUpBuffer      int
		IdleTimeoutMinutes int
	}
	Server struct {
		Port     string
		LogLevel string
	}
}

func Load() *Config {
	cfg := &Config{}

	// Redis configuration (same as other services)
	cfg.Redis.URL = getEnv("REDIS_URL", "redis:6379")
	cfg.Redis.Password = getEnv("REDIS_PASSWORD", "")

	// Node API configuration (connect to provided node-api service)
	cfg.NodeAPI.BaseURL = getEnv("NODE_API_URL", "http://node-api:8080")
	cfg.NodeAPI.Timeout = time.Duration(getEnvInt("NODE_API_TIMEOUT", 30)) * time.Second

	// Prediction algorithm configuration
	cfg.Prediction.WindowSizeMinutes = getEnvInt("PREDICTION_WINDOW_MINUTES", 5)
	cfg.Prediction.ConnectThreshold = getEnvFloat("CONNECT_THRESHOLD", 0.7)
	cfg.Prediction.ScaleUpBuffer = getEnvInt("SCALE_UP_BUFFER", 2)
	cfg.Prediction.IdleTimeoutMinutes = getEnvInt("IDLE_TIMEOUT_MINUTES", 10)

	// Server configuration
	cfg.Server.Port = getEnv("PORT", "8080")
	cfg.Server.LogLevel = getEnv("LOG_LEVEL", "info")

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}
