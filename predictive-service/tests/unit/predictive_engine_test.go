// predictive-service/tests/unit/predictive_engine_test.go
package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"

	"predictive-service/internal/config"
	"predictive-service/internal/models"
	"predictive-service/internal/services"
)

func TestPredictiveEngine_CalculateScalingDecision(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := &config.Config{
		Prediction: struct {
			WindowSizeMinutes  int
			ConnectThreshold   float64
			ScaleUpBuffer      int
			IdleTimeoutMinutes int
		}{
			WindowSizeMinutes:  5,
			ConnectThreshold:   0.7,
			ScaleUpBuffer:      2,
			IdleTimeoutMinutes: 10,
		},
	}

	// Create mock dependencies
	stateManager := &MockStateManager{}
	nodeManager := &MockNodePoolManager{}

	engine := services.NewPredictiveEngine(stateManager, nodeManager, cfg, logger)

	// Test the main public method
	decision := engine.CalculateScalingDecision()

	assert.NotNil(t, decision)
	assert.NotEmpty(t, decision.RecommendedAction)
	assert.GreaterOrEqual(t, decision.Confidence, 0.0)
	assert.LessOrEqual(t, decision.Confidence, 1.0)
	assert.NotZero(t, decision.Timestamp)
}

// Mock StateManager for testing
type MockStateManager struct{}

func (m *MockStateManager) GetActiveUsersInWindow(window time.Duration) []*models.UserActivity {
	// Return mock active users
	return []*models.UserActivity{
		{
			UserID:        "test-user-1",
			LastActivity:  time.Now().Add(-2 * time.Minute),
			ActivityCount: 5,
			SessionHistory: []models.SessionInfo{
				{
					ConnectTime: time.Now().Add(-1 * time.Hour),
					Duration:    30 * time.Minute,
				},
			},
		},
		{
			UserID:         "test-user-2",
			LastActivity:   time.Now().Add(-1 * time.Minute),
			ActivityCount:  8,
			SessionHistory: []models.SessionInfo{},
		},
	}
}

func (m *MockStateManager) GetReadyNodeCount() int {
	return 2
}

func (m *MockStateManager) GetBootingNodeCount() int {
	return 1
}

// Mock other required methods
func (m *MockStateManager) UpdateUserActivity(userID string, timestamp time.Time) {}
func (m *MockStateManager) AllocateNode(userID string) (string, error)            { return "node-123", nil }
func (m *MockStateManager) DeallocateNode(userID string)                          {}
func (m *MockStateManager) UpdateNodeStatus(nodeID, status string)                {}
func (m *MockStateManager) AddBootingNode(nodeID string)                          {}
func (m *MockStateManager) GetSystemMetrics() *models.SystemMetrics {
	return &models.SystemMetrics{
		TotalUsers:     2,
		ActiveUsers:    2,
		ReadyNodes:     2,
		AllocatedNodes: 1,
	}
}

// Mock NodePoolManager for testing
type MockNodePoolManager struct{}

func (m *MockNodePoolManager) ExecuteScalingDecision(decision *models.ScalingDecision) error {
	return nil
}

func (m *MockNodePoolManager) ProvisionNodeEmergency() (string, error) {
	return "emergency-node-123", nil
}

func TestPredictiveEngine_Integration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := &config.Config{
		Prediction: struct {
			WindowSizeMinutes  int
			ConnectThreshold   float64
			ScaleUpBuffer      int
			IdleTimeoutMinutes int
		}{
			WindowSizeMinutes:  5,
			ConnectThreshold:   0.7,
			ScaleUpBuffer:      2,
			IdleTimeoutMinutes: 10,
		},
	}

	stateManager := &MockStateManager{}
	nodeManager := &MockNodePoolManager{}

	engine := services.NewPredictiveEngine(stateManager, nodeManager, cfg, logger)

	// Test scaling decision with high activity
	decision := engine.CalculateScalingDecision()

	// Validate decision structure
	assert.Contains(t, []models.ScalingAction{
		models.ScaleUp, models.ScaleDown, models.Maintain,
	}, decision.RecommendedAction)

	assert.NotEmpty(t, decision.Reasoning)
	assert.GreaterOrEqual(t, decision.ExpectedConnections, 0)
}
