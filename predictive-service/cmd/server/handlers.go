// predictive-service/cmd/server/handlers.go
package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"predictive-service/internal/services"
)

func healthCheckHandler(stateManager *services.StateManager, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		health := map[string]interface{}{
			"status":    "UP",
			"timestamp": time.Now().UTC(),
			"checks":    make(map[string]interface{}),
		}

		// Check Redis connection
		err := redisClient.Ping(r.Context()).Err()
		if err != nil {
			health["status"] = "DOWN"
			health["checks"].(map[string]interface{})["redis"] = map[string]interface{}{
				"status": "DOWN",
				"error":  err.Error(),
			}
		} else {
			health["checks"].(map[string]interface{})["redis"] = map[string]interface{}{
				"status": "UP",
			}
		}

		// Add system metrics
		metrics := stateManager.GetSystemMetrics()
		health["metrics"] = metrics

		w.Header().Set("Content-Type", "application/json")
		if health["status"] == "DOWN" {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		json.NewEncoder(w).Encode(health)
	}
}

func statusHandler(stateManager *services.StateManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		metrics := stateManager.GetSystemMetrics()

		status := map[string]interface{}{
			"service":   "predictive-node-service",
			"version":   "1.0.0",
			"timestamp": time.Now().UTC(),
			"metrics":   metrics,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(status)
	}
}
