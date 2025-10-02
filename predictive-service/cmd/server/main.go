// predictive-service/cmd/server/main.go
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"predictive-service/internal/config"
	"predictive-service/internal/handlers"
	"predictive-service/internal/services"
)

func main() {
	// Load configuration from environment
	cfg := config.Load()

	// Initialize logger
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	logger.Info("starting_predictive_service",
		zap.String("redis_url", cfg.Redis.URL),
		zap.String("node_api_url", cfg.NodeAPI.BaseURL),
	)

	// Connect to Redis (same instance as other services)
	redisClient := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.URL,
		Password:     cfg.Redis.Password,
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	// Test Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Fatal("failed_to_connect_to_redis", zap.Error(err))
	}

	logger.Info("connected_to_redis")

	// Initialize services
	stateManager := services.NewStateManager(redisClient, logger)
	nodeAPIClient := services.NewNodeAPIClient(cfg.NodeAPI.BaseURL, logger)
	nodePoolManager := services.NewNodePoolManager(nodeAPIClient, stateManager, logger)
	predictor := services.NewPredictiveEngine(stateManager, nodePoolManager, cfg, logger)

	// Initialize event processor - THIS IS YOUR CORE COMPONENT
	eventProcessor := handlers.NewEventProcessor(
		redisClient,
		stateManager,
		nodePoolManager,
		predictor,
		logger,
	)

	// Start the event processing (subscribes to Redis channels)
	if err := eventProcessor.Start(); err != nil {
		logger.Fatal("failed_to_start_event_processor", zap.Error(err))
	}

	// Setup HTTP server for health checks and metrics
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthCheckHandler(stateManager, redisClient))
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/status", statusHandler(stateManager)) // Show current state

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("http_server_starting", zap.String("port", "8080"))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server_failed", zap.Error(err))
		}
	}()

	<-c
	logger.Info("shutting_down_gracefully")

	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	eventProcessor.Stop()
	server.Shutdown(ctx)
	redisClient.Close()

	logger.Info("shutdown_complete")
}
