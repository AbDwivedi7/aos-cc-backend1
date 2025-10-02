// predictive-service/internal/services/state_manager.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"predictive-service/internal/models"
)

type StateManager struct {
	mu          sync.RWMutex
	activeUsers map[string]*models.UserActivity
	nodes       map[string]*models.NodeState
	allocations map[string]string // userID -> nodeID
	readyNodes  []string
	metrics     *models.SystemMetrics

	redisClient *redis.Client
	logger      *zap.Logger
	ctx         context.Context
}

func NewStateManager(redisClient *redis.Client, logger *zap.Logger) *StateManager {
	sm := &StateManager{
		activeUsers: make(map[string]*models.UserActivity),
		nodes:       make(map[string]*models.NodeState),
		allocations: make(map[string]string),
		readyNodes:  make([]string, 0),
		metrics:     &models.SystemMetrics{},
		redisClient: redisClient,
		logger:      logger,
		ctx:         context.Background(),
	}

	// Load persisted state from Redis
	sm.loadPersistedState()

	return sm
}

func (sm *StateManager) UpdateUserActivity(userID string, timestamp time.Time) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	user, exists := sm.activeUsers[userID]
	if !exists {
		user = &models.UserActivity{
			UserID:         userID,
			SessionHistory: make([]models.SessionInfo, 0),
		}
		sm.activeUsers[userID] = user
	}

	user.LastActivity = timestamp
	user.ActivityCount++

	// Persist to Redis
	sm.persistUserActivity(userID, user)

	sm.logger.Debug("user_activity_updated",
		zap.String("user_id", userID),
		zap.Time("timestamp", timestamp),
		zap.Int("activity_count", user.ActivityCount),
	)
}

func (sm *StateManager) AllocateNode(userID string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Find first available ready node
	for i, nodeID := range sm.readyNodes {
		if node, exists := sm.nodes[nodeID]; exists && node.Status == models.NodeReady {
			// Atomically allocate
			node.Status = models.NodeAllocated
			node.AllocatedUser = userID
			now := time.Now()
			node.LastUsed = &now
			node.IdleSince = nil

			sm.allocations[userID] = nodeID
			// Remove from ready nodes slice
			sm.readyNodes = append(sm.readyNodes[:i], sm.readyNodes[i+1:]...)

			// Persist changes
			sm.persistNodeState(nodeID, node)
			sm.persistUserAllocation(userID, nodeID)

			sm.logger.Info("node_allocated",
				zap.String("user_id", userID),
				zap.String("node_id", nodeID),
			)

			return nodeID, nil
		}
	}

	return "", fmt.Errorf("no available ready nodes")
}

func (sm *StateManager) DeallocateNode(userID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	nodeID, exists := sm.allocations[userID]
	if !exists {
		return
	}

	if node, exists := sm.nodes[nodeID]; exists {
		node.Status = models.NodeReady
		node.AllocatedUser = ""
		now := time.Now()
		node.IdleSince = &now

		// Add back to ready nodes
		sm.readyNodes = append(sm.readyNodes, nodeID)

		// Update user session history
		if user, exists := sm.activeUsers[userID]; exists && user.LastConnectTime != nil {
			sessionInfo := models.SessionInfo{
				ConnectTime:    *user.LastConnectTime,
				DisconnectTime: &now,
				Duration:       now.Sub(*user.LastConnectTime),
				NodeID:         nodeID,
			}
			user.SessionHistory = append(user.SessionHistory, sessionInfo)
			user.LastConnectTime = nil
			sm.persistUserActivity(userID, user)
		}

		sm.persistNodeState(nodeID, node)
	}

	delete(sm.allocations, userID)
	sm.removeUserAllocation(userID)

	sm.logger.Info("node_deallocated",
		zap.String("user_id", userID),
		zap.String("node_id", nodeID),
	)
}

func (sm *StateManager) UpdateNodeStatus(nodeID, status string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	node, exists := sm.nodes[nodeID]
	if !exists {
		node = &models.NodeState{
			NodeID:    nodeID,
			CreatedAt: time.Now(),
		}
		sm.nodes[nodeID] = node
	}

	oldStatus := node.Status
	node.Status = models.NodeStatus(status)

	// Handle status transitions
	switch models.NodeStatus(status) {
	case models.NodeReady:
		if oldStatus == models.NodeBooting {
			// Node just became ready
			sm.readyNodes = append(sm.readyNodes, nodeID)
			now := time.Now()
			node.IdleSince = &now
		}
	case models.NodeTerminated:
		// Remove from ready nodes if present
		sm.removeFromReadyNodes(nodeID)
	}

	sm.persistNodeState(nodeID, node)

	sm.logger.Debug("node_status_updated",
		zap.String("node_id", nodeID),
		zap.String("old_status", string(oldStatus)),
		zap.String("new_status", status),
	)
}

func (sm *StateManager) AddBootingNode(nodeID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	node := &models.NodeState{
		NodeID:    nodeID,
		Status:    models.NodeBooting,
		CreatedAt: time.Now(),
	}

	sm.nodes[nodeID] = node
	sm.persistNodeState(nodeID, node)

	sm.logger.Info("booting_node_added", zap.String("node_id", nodeID))
}

func (sm *StateManager) GetActiveUsersInWindow(window time.Duration) []*models.UserActivity {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	cutoff := time.Now().Add(-window)
	var activeUsers []*models.UserActivity

	for _, user := range sm.activeUsers {
		if user.LastActivity.After(cutoff) {
			activeUsers = append(activeUsers, user)
		}
	}

	return activeUsers
}

func (sm *StateManager) GetReadyNodeCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.readyNodes)
}

func (sm *StateManager) GetBootingNodeCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	count := 0
	for _, node := range sm.nodes {
		if node.Status == models.NodeBooting {
			count++
		}
	}
	return count
}

func (sm *StateManager) GetSystemMetrics() *models.SystemMetrics {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	readyCount := 0
	allocatedCount := 0

	for _, node := range sm.nodes {
		switch node.Status {
		case models.NodeReady:
			readyCount++
		case models.NodeAllocated:
			allocatedCount++
		}
	}

	return &models.SystemMetrics{
		TotalUsers:     len(sm.activeUsers),
		ActiveUsers:    len(sm.GetActiveUsersInWindow(5 * time.Minute)),
		TotalNodes:     len(sm.nodes),
		ReadyNodes:     readyCount,
		AllocatedNodes: allocatedCount,
	}
}

// Redis persistence methods
func (sm *StateManager) persistUserActivity(userID string, activity *models.UserActivity) {
	key := fmt.Sprintf("user:activity:%s", userID)
	data, err := json.Marshal(activity)
	if err != nil {
		sm.logger.Error("failed_to_marshal_user_activity", zap.Error(err))
		return
	}

	sm.redisClient.Set(sm.ctx, key, data, 24*time.Hour).Err()
}

func (sm *StateManager) persistNodeState(nodeID string, state *models.NodeState) {
	key := fmt.Sprintf("node:state:%s", nodeID)
	data, err := json.Marshal(state)
	if err != nil {
		sm.logger.Error("failed_to_marshal_node_state", zap.Error(err))
		return
	}

	sm.redisClient.Set(sm.ctx, key, data, 0).Err()
}

func (sm *StateManager) persistUserAllocation(userID, nodeID string) {
	key := fmt.Sprintf("allocation:%s", userID)
	sm.redisClient.Set(sm.ctx, key, nodeID, 0).Err()
}

func (sm *StateManager) removeUserAllocation(userID string) {
	key := fmt.Sprintf("allocation:%s", userID)
	sm.redisClient.Del(sm.ctx, key).Err()
}

func (sm *StateManager) loadPersistedState() {
	// Load user activities
	userKeys := sm.redisClient.Keys(sm.ctx, "user:activity:*").Val()
	for _, key := range userKeys {
		data := sm.redisClient.Get(sm.ctx, key).Val()
		var activity models.UserActivity
		if err := json.Unmarshal([]byte(data), &activity); err == nil {
			sm.activeUsers[activity.UserID] = &activity
		}
	}

	// Load node states
	nodeKeys := sm.redisClient.Keys(sm.ctx, "node:state:*").Val()
	for _, key := range nodeKeys {
		data := sm.redisClient.Get(sm.ctx, key).Val()
		var state models.NodeState
		if err := json.Unmarshal([]byte(data), &state); err == nil {
			sm.nodes[state.NodeID] = &state
			if state.Status == models.NodeReady {
				sm.readyNodes = append(sm.readyNodes, state.NodeID)
			}
		}
	}

	// Load allocations
	allocKeys := sm.redisClient.Keys(sm.ctx, "allocation:*").Val()
	for _, key := range allocKeys {
		userID := key[11:] // Remove "allocation:" prefix
		nodeID := sm.redisClient.Get(sm.ctx, key).Val()
		if nodeID != "" {
			sm.allocations[userID] = nodeID
		}
	}

	sm.logger.Info("state_loaded_from_redis",
		zap.Int("users", len(sm.activeUsers)),
		zap.Int("nodes", len(sm.nodes)),
		zap.Int("allocations", len(sm.allocations)),
	)
}

func (sm *StateManager) removeFromReadyNodes(nodeID string) {
	for i, id := range sm.readyNodes {
		if id == nodeID {
			sm.readyNodes = append(sm.readyNodes[:i], sm.readyNodes[i+1:]...)
			break
		}
	}
}
