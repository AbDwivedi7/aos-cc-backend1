// predictive-service/internal/services/predictive_engine.go
package services

import (
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"

	"predictive-service/internal/config"
	"predictive-service/internal/models"
)

type PredictiveEngine struct {
	// stateManager *StateManager
	// nodeManager  *NodePoolManager
	stateManager StateManagerInterface
	nodeManager  NodePoolManagerInterface
	config       *config.Config
	logger       *zap.Logger

	// Algorithm parameters
	windowSize       time.Duration
	connectThreshold float64
	scaleUpBuffer    int
	timeWeights      map[int]float64
}

func NewPredictiveEngine(
	// stateManager *StateManager,
	// nodeManager *NodePoolManager,
	stateManager StateManagerInterface,
	nodeManager NodePoolManagerInterface,
	cfg *config.Config, // Keep this as *config.Config
	logger *zap.Logger,
) *PredictiveEngine {
	// Initialize time-based weights (higher during peak hours)
	timeWeights := map[int]float64{
		// Peak hours get higher weights
		9: 1.3, 10: 1.4, 11: 1.5, // Morning peak
		14: 1.2, 15: 1.3, 16: 1.4, // Afternoon peak
		19: 1.2, 20: 1.3, 21: 1.4, // Evening peak
	}

	// Fill in normal hours with weight 1.0
	for i := 0; i < 24; i++ {
		if _, exists := timeWeights[i]; !exists {
			timeWeights[i] = 1.0
		}
	}

	return &PredictiveEngine{
		stateManager:     stateManager,
		nodeManager:      nodeManager,
		config:           cfg,
		logger:           logger,
		windowSize:       time.Duration(cfg.Prediction.WindowSizeMinutes) * time.Minute,
		connectThreshold: cfg.Prediction.ConnectThreshold,
		scaleUpBuffer:    cfg.Prediction.ScaleUpBuffer,
		timeWeights:      timeWeights,
	}
}

func (pe *PredictiveEngine) CalculateScalingDecision() *models.ScalingDecision {
	// Step 1: Get active users in prediction window
	activeUsers := pe.stateManager.GetActiveUsersInWindow(pe.windowSize)

	// Step 2: Calculate individual user connection probabilities
	totalConnectionProbability := 0.0
	userProbabilities := make(map[string]float64)

	for _, user := range activeUsers {
		probability := pe.calculateUserConnectionProbability(user)
		userProbabilities[user.UserID] = probability
		totalConnectionProbability += probability
	}

	// Step 3: Apply contextual weights
	adjustedProbability := pe.applyContextualWeights(totalConnectionProbability)

	// Step 4: Current node pool analysis
	readyNodes := pe.stateManager.GetReadyNodeCount()
	bootingNodes := pe.stateManager.GetBootingNodeCount()
	expectedDemand := int(math.Ceil(adjustedProbability))

	// Step 5: Make scaling decision
	action := pe.determineScalingAction(expectedDemand, readyNodes, bootingNodes)
	confidence := pe.calculateConfidence(userProbabilities)
	reasoning := pe.generateReasoning(expectedDemand, readyNodes, bootingNodes, action)

	decision := &models.ScalingDecision{
		ExpectedConnections: expectedDemand,
		CurrentReadyNodes:   readyNodes,
		CurrentBootingNodes: bootingNodes,
		RecommendedAction:   action,
		Confidence:          confidence,
		Timestamp:           time.Now(),
		Reasoning:           reasoning,
	}

	pe.logger.Info("scaling_decision_calculated",
		zap.Int("expected_connections", expectedDemand),
		zap.Int("ready_nodes", readyNodes),
		zap.Int("booting_nodes", bootingNodes),
		zap.String("action", string(action)),
		zap.Float64("confidence", confidence),
		zap.String("reasoning", reasoning),
	)

	return decision
}

func (pe *PredictiveEngine) calculateUserConnectionProbability(user *models.UserActivity) float64 {
	// Factor 1: Recent activity frequency (40% weight)
	recentActivityScore := pe.calculateRecentActivityScore(user)

	// Factor 2: Historical connection rate (30% weight)
	historicalScore := pe.calculateHistoricalScore(user)

	// Factor 3: Time-based pattern (20% weight)
	timeScore := pe.calculateTimeBasedScore()

	// Factor 4: Session pattern (10% weight)
	sessionScore := pe.calculateSessionPatternScore(user)

	probability := (recentActivityScore * 0.4) +
		(historicalScore * 0.3) +
		(timeScore * 0.2) +
		(sessionScore * 0.1)

	// Apply exponential decay for time since last activity
	timeSinceActivity := time.Since(user.LastActivity)
	decayFactor := math.Exp(-timeSinceActivity.Minutes() / 10.0) // 10-minute half-life

	finalProbability := probability * decayFactor

	// Ensure probability is between 0 and 1
	if finalProbability > 1.0 {
		finalProbability = 1.0
	} else if finalProbability < 0.0 {
		finalProbability = 0.0
	}

	return finalProbability
}

func (pe *PredictiveEngine) calculateRecentActivityScore(user *models.UserActivity) float64 {
	// Higher activity count in recent window = higher connection probability
	// Normalize activity count to 0-1 scale
	activityScore := float64(user.ActivityCount) / 20.0 // Assume 20 activities = very high
	if activityScore > 1.0 {
		activityScore = 1.0
	}
	return activityScore
}

func (pe *PredictiveEngine) calculateHistoricalScore(user *models.UserActivity) float64 {
	if len(user.SessionHistory) == 0 {
		return 0.5 // Default for new users
	}

	// Calculate both total and recent sessions
	totalSessions := len(user.SessionHistory)
	recentSessions := 0
	cutoff := time.Now().Add(-7 * 24 * time.Hour) // Last 7 days

	for _, session := range user.SessionHistory {
		if session.ConnectTime.After(cutoff) {
			recentSessions++
		}
	}

	if recentSessions == 0 {
		return 0.3 // Low probability for inactive users
	}

	// Combine recent activity with overall historical pattern
	recentScore := math.Min(float64(recentSessions)/10.0, 1.0)

	// Users with more total history get slight boost (experience factor)
	experienceBoost := math.Min(float64(totalSessions)/50.0, 0.2) // Max 20% boost

	finalScore := recentScore + experienceBoost
	return math.Min(finalScore, 1.0) // Cap at 1.0
}

func (pe *PredictiveEngine) calculateTimeBasedScore() float64 {
	currentHour := time.Now().Hour()
	weight := pe.timeWeights[currentHour]

	// Convert weight to probability score (1.0 = normal, 1.5 = 50% higher probability)
	return math.Min(weight/1.5, 1.0)
}

func (pe *PredictiveEngine) calculateSessionPatternScore(user *models.UserActivity) float64 {
	if len(user.SessionHistory) < 3 {
		return 0.5 // Default for users with limited history
	}

	// Analyze if user has consistent connection patterns
	currentTime := time.Now()
	currentHour := currentTime.Hour()
	currentWeekday := currentTime.Weekday()

	matchingPatterns := 0
	for _, session := range user.SessionHistory {
		sessionHour := session.ConnectTime.Hour()
		sessionWeekday := session.ConnectTime.Weekday()

		// Check if session was at similar time
		if math.Abs(float64(sessionHour-currentHour)) <= 2 && sessionWeekday == currentWeekday {
			matchingPatterns++
		}
	}

	patternScore := float64(matchingPatterns) / float64(len(user.SessionHistory))
	return math.Min(patternScore, 1.0)
}

func (pe *PredictiveEngine) applyContextualWeights(baseProbability float64) float64 {
	currentHour := time.Now().Hour()
	timeWeight := pe.timeWeights[currentHour]

	return baseProbability * timeWeight
}

func (pe *PredictiveEngine) determineScalingAction(expectedDemand, readyNodes, bootingNodes int) models.ScalingAction {
	totalAvailable := readyNodes + bootingNodes
	requiredNodes := expectedDemand + pe.scaleUpBuffer

	if totalAvailable < requiredNodes {
		return models.ScaleUp
	} else if readyNodes > expectedDemand+pe.scaleUpBuffer+2 {
		// Too many idle nodes, scale down
		return models.ScaleDown
	} else {
		return models.Maintain
	}
}

func (pe *PredictiveEngine) calculateConfidence(userProbabilities map[string]float64) float64 {
	if len(userProbabilities) == 0 {
		return 0.5
	}

	// Calculate variance in probabilities - lower variance = higher confidence
	sum := 0.0
	for _, prob := range userProbabilities {
		sum += prob
	}
	mean := sum / float64(len(userProbabilities))

	variance := 0.0
	for _, prob := range userProbabilities {
		variance += math.Pow(prob-mean, 2)
	}
	variance /= float64(len(userProbabilities))

	// Convert variance to confidence (lower variance = higher confidence)
	confidence := 1.0 - math.Min(variance, 1.0)
	return confidence
}

func (pe *PredictiveEngine) generateReasoning(expectedDemand, readyNodes, bootingNodes int, action models.ScalingAction) string {
	switch action {
	case models.ScaleUp:
		return fmt.Sprintf("Expected %d connections but only %d ready + %d booting nodes. Scaling up to meet demand.",
			expectedDemand, readyNodes, bootingNodes)
	case models.ScaleDown:
		return fmt.Sprintf("Have %d ready nodes for expected %d connections. Scaling down to optimize costs.",
			readyNodes, expectedDemand)
	default:
		return fmt.Sprintf("Current capacity (%d ready + %d booting) adequate for expected %d connections.",
			readyNodes, bootingNodes, expectedDemand)
	}
}

func (pe *PredictiveEngine) AnalyzeActivityPattern(userID string) {
	// This can be called when user activity is detected
	// Can be used for real-time pattern analysis
	pe.logger.Debug("analyzing_activity_pattern", zap.String("user_id", userID))
}

func (pe *PredictiveEngine) TriggerScalingDecision() {
	// This can be called to trigger immediate scaling decision
	decision := pe.CalculateScalingDecision()
	pe.nodeManager.ExecuteScalingDecision(decision)
}
