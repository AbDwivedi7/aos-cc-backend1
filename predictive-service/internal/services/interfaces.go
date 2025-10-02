// predictive-service/internal/services/interfaces.go
package services

import (
	"predictive-service/internal/models"
	"time"
)

type StateManagerInterface interface {
	GetActiveUsersInWindow(window time.Duration) []*models.UserActivity
	GetReadyNodeCount() int
	GetBootingNodeCount() int
	UpdateUserActivity(userID string, timestamp time.Time)
	AllocateNode(userID string) (string, error)
	DeallocateNode(userID string)
	UpdateNodeStatus(nodeID, status string)
	AddBootingNode(nodeID string)
	GetSystemMetrics() *models.SystemMetrics
}

type NodePoolManagerInterface interface {
	ExecuteScalingDecision(decision *models.ScalingDecision) error
	ProvisionNodeEmergency() (string, error)
}
