// predictive-service/internal/models/state.go
package models

import "time"

type UserActivity struct {
	UserID          string        `json:"user_id"`
	LastActivity    time.Time     `json:"last_activity"`
	ActivityCount   int           `json:"activity_count"`
	SessionHistory  []SessionInfo `json:"session_history"`
	PredictionScore float64       `json:"prediction_score"`
	LastConnectTime *time.Time    `json:"last_connect_time,omitempty"`
}

type SessionInfo struct {
	ConnectTime    time.Time     `json:"connect_time"`
	DisconnectTime *time.Time    `json:"disconnect_time,omitempty"`
	Duration       time.Duration `json:"duration"`
	NodeID         string        `json:"node_id"`
}

type NodeState struct {
	NodeID        string     `json:"node_id"`
	Status        NodeStatus `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	LastUsed      *time.Time `json:"last_used,omitempty"`
	AllocatedUser string     `json:"allocated_user,omitempty"`
	IdleSince     *time.Time `json:"idle_since,omitempty"`
}

type NodeStatus string

const (
	NodeBooting    NodeStatus = "booting"
	NodeReady      NodeStatus = "ready"
	NodeAllocated  NodeStatus = "allocated"
	NodeTerminated NodeStatus = "terminated"
)

type ScalingDecision struct {
	ExpectedConnections int           `json:"expected_connections"`
	CurrentReadyNodes   int           `json:"current_ready_nodes"`
	CurrentBootingNodes int           `json:"current_booting_nodes"`
	RecommendedAction   ScalingAction `json:"recommended_action"`
	Confidence          float64       `json:"confidence"`
	Timestamp           time.Time     `json:"timestamp"`
	Reasoning           string        `json:"reasoning"`
}

type ScalingAction string

const (
	ScaleUp   ScalingAction = "scale_up"
	ScaleDown ScalingAction = "scale_down"
	Maintain  ScalingAction = "maintain"
)

type SystemMetrics struct {
	TotalUsers            int     `json:"total_users"`
	ActiveUsers           int     `json:"active_users"`
	TotalNodes            int     `json:"total_nodes"`
	ReadyNodes            int     `json:"ready_nodes"`
	AllocatedNodes        int     `json:"allocated_nodes"`
	ConnectionSuccessRate float64 `json:"connection_success_rate"`
	PredictionAccuracy    float64 `json:"prediction_accuracy"`
}
