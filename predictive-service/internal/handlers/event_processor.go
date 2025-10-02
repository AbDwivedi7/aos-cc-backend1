// predictive-service/internal/handlers/event_processor.go
package handlers

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"predictive-service/internal/models"
	"predictive-service/internal/services"
)

type EventProcessor struct {
	redisClient  *redis.Client
	stateManager *services.StateManager
	nodeManager  *services.NodePoolManager
	predictor    *services.PredictiveEngine
	logger       *zap.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	eventsProcessed map[string]int64
	mu              sync.RWMutex
}

func NewEventProcessor(
	redisClient *redis.Client,
	stateManager *services.StateManager,
	nodeManager *services.NodePoolManager,
	predictor *services.PredictiveEngine,
	logger *zap.Logger,
) *EventProcessor {
	return &EventProcessor{
		redisClient:     redisClient,
		stateManager:    stateManager,
		nodeManager:     nodeManager,
		predictor:       predictor,
		logger:          logger,
		eventsProcessed: make(map[string]int64),
	}
}

func (ep *EventProcessor) Start() error {
	ep.ctx, ep.cancel = context.WithCancel(context.Background())

	// Subscribe to all required channels that user-simulator publishes to
	pubsub := ep.redisClient.Subscribe(ep.ctx,
		"user:activity",
		"user:connect",
		"user:disconnect",
		"node:status",
	)

	ep.logger.Info("subscribed_to_redis_channels",
		zap.Strings("channels", []string{
			"user:activity", "user:connect", "user:disconnect", "node:status",
		}),
	)

	// Start processing goroutines for each channel
	ch := pubsub.Channel()

	ep.wg.Add(1)
	go ep.processEvents(ch)

	// Start predictive scaling loop
	ep.wg.Add(1)
	go ep.runPredictiveScaling()

	return nil
}

func (ep *EventProcessor) processEvents(ch <-chan *redis.Message) {
	defer ep.wg.Done()

	for {
		select {
		case msg := <-ch:
			ep.handleMessage(msg)
		case <-ep.ctx.Done():
			ep.logger.Info("event_processor_stopping")
			return
		}
	}
}

func (ep *EventProcessor) handleMessage(msg *redis.Message) {
	ep.incrementEventCount(msg.Channel)

	switch msg.Channel {
	case "user:activity":
		ep.handleUserActivity(msg.Payload)
	case "user:connect":
		ep.handleUserConnect(msg.Payload)
	case "user:disconnect":
		ep.handleUserDisconnect(msg.Payload)
	case "node:status":
		ep.handleNodeStatus(msg.Payload)
	default:
		ep.logger.Warn("unknown_channel", zap.String("channel", msg.Channel))
	}
}

func (ep *EventProcessor) handleUserActivity(payload string) {
	var event models.UserActivityEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		ep.logger.Error("failed_to_unmarshal_user_activity", zap.Error(err))
		return
	}

	// Update user activity state
	ep.stateManager.UpdateUserActivity(event.UserID, time.Unix(event.Timestamp, 0))

	// Trigger predictive analysis
	ep.predictor.AnalyzeActivityPattern(event.UserID)

	ep.logger.Debug("user_activity_processed",
		zap.String("user_id", event.UserID),
		zap.Int64("timestamp", event.Timestamp),
	)
}

func (ep *EventProcessor) handleUserConnect(payload string) {
	var event models.UserConnectEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		ep.logger.Error("failed_to_unmarshal_user_connect", zap.Error(err))
		return
	}

	// CRITICAL: User needs immediate node allocation
	start := time.Now()
	nodeID, err := ep.stateManager.AllocateNode(event.UserID)
	duration := time.Since(start)

	if err != nil {
		ep.logger.Error("user_connect_failed",
			zap.String("user_id", event.UserID),
			zap.Error(err),
			zap.Duration("attempt_duration", duration),
		)

		// Emergency: Try to create node immediately (reactive scaling)
		go ep.nodeManager.ProvisionNodeEmergency()

	} else {
		ep.logger.Info("user_connect_success",
			zap.String("user_id", event.UserID),
			zap.String("node_id", nodeID),
			zap.Duration("allocation_time", duration),
		)

		// Trigger predictive scaling for future demand
		go ep.predictor.TriggerScalingDecision()
	}
}

func (ep *EventProcessor) handleUserDisconnect(payload string) {
	var event models.UserDisconnectEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		ep.logger.Error("failed_to_unmarshal_user_disconnect", zap.Error(err))
		return
	}

	// Free up the node for reuse
	ep.stateManager.DeallocateNode(event.UserID)

	ep.logger.Info("user_disconnected",
		zap.String("user_id", event.UserID),
	)
}

func (ep *EventProcessor) handleNodeStatus(payload string) {
	var event models.NodeStatusEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		ep.logger.Error("failed_to_unmarshal_node_status", zap.Error(err))
		return
	}

	// Update node state based on status from node-api
	ep.stateManager.UpdateNodeStatus(event.NodeID, event.Status)

	ep.logger.Debug("node_status_updated",
		zap.String("node_id", event.NodeID),
		zap.String("status", event.Status),
	)
}

func (ep *EventProcessor) runPredictiveScaling() {
	defer ep.wg.Done()

	ticker := time.NewTicker(30 * time.Second) // Run every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			decision := ep.predictor.CalculateScalingDecision()
			if err := ep.nodeManager.ExecuteScalingDecision(decision); err != nil {
				ep.logger.Error("scaling_decision_failed", zap.Error(err))
			}
		case <-ep.ctx.Done():
			return
		}
	}
}

func (ep *EventProcessor) Stop() {
	ep.logger.Info("stopping_event_processor")
	if ep.cancel != nil {
		ep.cancel()
	}
	ep.wg.Wait()
	ep.logger.Info("event_processor_stopped")
}

func (ep *EventProcessor) incrementEventCount(channel string) {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	ep.eventsProcessed[channel]++
}

func (ep *EventProcessor) GetEventCounts() map[string]int64 {
	ep.mu.RLock()
	defer ep.mu.RUnlock()

	counts := make(map[string]int64)
	for k, v := range ep.eventsProcessed {
		counts[k] = v
	}
	return counts
}
