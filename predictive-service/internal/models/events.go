// predictive-service/internal/models/events.go
package models

type UserActivityEvent struct {
	UserID    string `json:"user_id"`
	Timestamp int64  `json:"timestamp"`
}

type UserConnectEvent struct {
	UserID string `json:"user_id"`
}

type UserDisconnectEvent struct {
	UserID string `json:"user_id"`
}

type NodeStatusEvent struct {
	NodeID string `json:"node_id"`
	Status string `json:"status"` // booting|ready|terminated
}
