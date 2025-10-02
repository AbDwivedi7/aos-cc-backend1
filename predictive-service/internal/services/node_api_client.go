// predictive-service/internal/services/node_api_client.go
package services

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"go.uber.org/zap"
)

type NodeAPIClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *zap.Logger
}

type NodeCreationResponse struct {
	ID string `json:"id"`
}

func NewNodeAPIClient(baseURL string, logger *zap.Logger) *NodeAPIClient {
	return &NodeAPIClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

func (nac *NodeAPIClient) CreateNode() (*NodeCreationResponse, error) {
	url := fmt.Sprintf("%s/api/nodes", nac.baseURL)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "predictive-service/1.0")

	// Implement retry logic with exponential backoff
	var response *NodeCreationResponse
	err = nac.retryWithBackoff(func() error {
		resp, err := nac.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("http request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 202 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
		}

		return json.NewDecoder(resp.Body).Decode(&response)
	}, 3) // 3 retries

	if err != nil {
		nac.logger.Error("node_creation_api_failed", zap.Error(err))
		return nil, err
	}

	nac.logger.Info("node_creation_api_success", zap.String("node_id", response.ID))
	return response, nil
}

func (nac *NodeAPIClient) DeleteNode(nodeID string) error {
	url := fmt.Sprintf("%s/api/nodes/%s", nac.baseURL, nodeID)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}

	req.Header.Set("User-Agent", "predictive-service/1.0")

	// Implement retry logic
	err = nac.retryWithBackoff(func() error {
		resp, err := nac.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("http request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 202 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
		}

		return nil
	}, 3) // 3 retries

	if err != nil {
		nac.logger.Error("node_deletion_api_failed",
			zap.String("node_id", nodeID),
			zap.Error(err),
		)
		return err
	}

	nac.logger.Info("node_deletion_api_success", zap.String("node_id", nodeID))
	return nil
}

func (nac *NodeAPIClient) retryWithBackoff(operation func() error, maxRetries int) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		err = operation()
		if err == nil {
			return nil
		}

		if i < maxRetries-1 {
			backoff := time.Duration(math.Pow(2, float64(i))) * time.Second
			nac.logger.Warn("api_operation_failed_retrying",
				zap.Error(err),
				zap.Int("retry", i+1),
				zap.Duration("backoff", backoff),
			)
			time.Sleep(backoff)
		}
	}

	return fmt.Errorf("operation failed after %d retries: %w", maxRetries, err)
}
