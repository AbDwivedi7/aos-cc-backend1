// predictive-service/internal/services/node_pool_manager.go
package services

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"predictive-service/internal/models"
)

type NodePoolManager struct {
	apiClient    *NodeAPIClient
	stateManager *StateManager
	logger       *zap.Logger

	// Scaling policies
	minNodes           int
	maxNodes           int
	idleTimeout        time.Duration
	maxScaleUpPerCycle int

	// Prevent concurrent scaling operations
	scalingMutex sync.Mutex
}

func NewNodePoolManager(apiClient *NodeAPIClient, stateManager *StateManager, logger *zap.Logger) *NodePoolManager {
	return &NodePoolManager{
		apiClient:          apiClient,
		stateManager:       stateManager,
		logger:             logger,
		minNodes:           1,
		maxNodes:           20,
		idleTimeout:        10 * time.Minute,
		maxScaleUpPerCycle: 5,
	}
}

func (npm *NodePoolManager) ExecuteScalingDecision(decision *models.ScalingDecision) error {
	npm.scalingMutex.Lock()
	defer npm.scalingMutex.Unlock()

	switch decision.RecommendedAction {
	case models.ScaleUp:
		return npm.scaleUp(decision)
	case models.ScaleDown:
		return npm.scaleDown(decision)
	case models.Maintain:
		return npm.maintainPool()
	default:
		return fmt.Errorf("unknown scaling action: %s", decision.RecommendedAction)
	}
}

func (npm *NodePoolManager) scaleUp(decision *models.ScalingDecision) error {
	currentReady := decision.CurrentReadyNodes
	currentBooting := decision.CurrentBootingNodes
	expectedConnections := decision.ExpectedConnections

	// Calculate how many additional nodes we need
	totalRequired := expectedConnections + 2 // Buffer of 2 nodes
	totalCurrent := currentReady + currentBooting

	if totalCurrent >= totalRequired {
		npm.logger.Debug("no_scaling_required",
			zap.Int("total_current", totalCurrent),
			zap.Int("total_required", totalRequired),
		)
		return nil
	}

	nodesToCreate := totalRequired - totalCurrent
	if nodesToCreate > npm.maxScaleUpPerCycle {
		nodesToCreate = npm.maxScaleUpPerCycle
	}

	npm.logger.Info("scaling_up_nodes",
		zap.Int("nodes_to_create", nodesToCreate),
		zap.Int("expected_connections", expectedConnections),
		zap.Int("current_ready", currentReady),
		zap.Int("current_booting", currentBooting),
	)

	// Create nodes concurrently but with controlled parallelism
	semaphore := make(chan struct{}, 3) // Max 3 concurrent API calls
	var wg sync.WaitGroup
	errors := make(chan error, nodesToCreate)

	for i := 0; i < nodesToCreate; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			resp, err := npm.apiClient.CreateNode()
			if err != nil {
				errors <- fmt.Errorf("failed to create node %d: %w", index, err)
				return
			}

			// Register new node in booting state
			npm.stateManager.AddBootingNode(resp.ID)

			npm.logger.Info("node_creation_initiated",
				zap.String("node_id", resp.ID),
				zap.Int("index", index),
			)
		}(i)
	}

	wg.Wait()
	close(errors)

	// Collect any errors
	var errorCount int
	for err := range errors {
		errorCount++
		npm.logger.Error("node_creation_failed", zap.Error(err))
	}

	if errorCount > 0 {
		return fmt.Errorf("failed to create %d out of %d nodes", errorCount, nodesToCreate)
	}

	npm.logger.Info("scale_up_completed", zap.Int("nodes_created", nodesToCreate))
	return nil
}

func (npm *NodePoolManager) scaleDown(decision *models.ScalingDecision) error {
	// Find idle nodes that can be terminated
	idleNodes := npm.findIdleNodes()

	if len(idleNodes) == 0 {
		npm.logger.Debug("no_idle_nodes_to_terminate")
		return nil
	}

	// Don't terminate too many nodes at once
	nodesToTerminate := len(idleNodes)
	if nodesToTerminate > 3 {
		nodesToTerminate = 3
	}

	npm.logger.Info("scaling_down_nodes", zap.Int("nodes_to_terminate", nodesToTerminate))

	for i := 0; i < nodesToTerminate; i++ {
		nodeID := idleNodes[i]

		err := npm.apiClient.DeleteNode(nodeID)
		if err != nil {
			npm.logger.Error("failed_to_delete_node",
				zap.String("node_id", nodeID),
				zap.Error(err),
			)
			continue
		}

		npm.logger.Info("node_termination_initiated", zap.String("node_id", nodeID))
	}

	return nil
}

func (npm *NodePoolManager) maintainPool() error {
	// Perform maintenance tasks
	npm.logger.Debug("maintaining_node_pool")

	// Check for nodes that have been idle too long
	npm.cleanupIdleNodes()

	return nil
}

func (npm *NodePoolManager) findIdleNodes() []string {
	metrics := npm.stateManager.GetSystemMetrics()

	// Simple heuristic: if we have more than expected ready nodes, some are idle
	expectedReady := 2 // Minimum buffer
	if metrics.ReadyNodes > expectedReady {
		// This is a simplified implementation
		// In a real system, you'd track actual idle times per node
		excess := metrics.ReadyNodes - expectedReady
		idleNodes := make([]string, 0, excess)

		// Get some ready nodes to terminate (simplified)
		// In reality, you'd get the actual idle node IDs from state manager
		for i := 0; i < excess; i++ {
			idleNodes = append(idleNodes, fmt.Sprintf("idle-node-%d", i))
		}

		return idleNodes
	}

	return []string{}
}

func (npm *NodePoolManager) cleanupIdleNodes() {
	// This would identify nodes that have been idle longer than the threshold
	// and mark them for termination
	npm.logger.Debug("cleaning_up_idle_nodes")
}

func (npm *NodePoolManager) ProvisionNodeEmergency() (string, error) {
	// Emergency provisioning when a user connect fails
	npm.logger.Warn("emergency_node_provisioning_triggered")

	resp, err := npm.apiClient.CreateNode()
	if err != nil {
		return "", fmt.Errorf("emergency provision failed: %w", err)
	}

	npm.stateManager.AddBootingNode(resp.ID)

	npm.logger.Info("emergency_node_provisioned", zap.String("node_id", resp.ID))
	return resp.ID, nil
}
