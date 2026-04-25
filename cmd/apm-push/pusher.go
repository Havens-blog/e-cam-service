package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"time"
)

// Pusher pushes LinkDeclarations to the topology API endpoint.
type Pusher interface {
	// Push sends a single LinkDeclaration to the topology API.
	Push(ctx context.Context, decl LinkDeclaration) error
	// PushAll sends all declarations, logging errors but continuing on failure.
	PushAll(ctx context.Context, declarations []LinkDeclaration)
}

// HTTPPusher implements Pusher using HTTP POST requests.
type HTTPPusher struct {
	apiURL   string
	tenantID string
	client   *http.Client
}

// NewHTTPPusher creates a new HTTP pusher targeting the given topology API URL.
func NewHTTPPusher(apiURL, tenantID string) *HTTPPusher {
	return &HTTPPusher{
		apiURL:   apiURL,
		tenantID: tenantID,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewHTTPPusherWithClient creates a pusher with a custom HTTP client (for testing).
func NewHTTPPusherWithClient(apiURL, tenantID string, client *http.Client) *HTTPPusher {
	return &HTTPPusher{
		apiURL:   apiURL,
		tenantID: tenantID,
		client:   client,
	}
}

const (
	maxRetries     = 3
	baseRetryDelay = 1 * time.Second
)

// Push sends a single LinkDeclaration to the topology API with retry logic.
// Retries up to 3 times with exponential backoff for network errors.
// Non-200 responses are logged but not retried (server-side errors).
func (p *HTTPPusher) Push(ctx context.Context, decl LinkDeclaration) error {
	body, err := json.Marshal(decl)
	if err != nil {
		return fmt.Errorf("failed to marshal declaration: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := baseRetryDelay * time.Duration(math.Pow(2, float64(attempt-1)))
			log.Printf("Retrying push (attempt %d/%d) after %v", attempt, maxRetries, delay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", p.tenantID)

		resp, err := p.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue // Retry on network errors
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return nil // Success
		}

		// Non-200: log and don't retry (server understood the request)
		log.Printf("ERROR: push declaration for node %s returned status %d: %s",
			decl.Node.ID, resp.StatusCode, string(respBody))
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	return fmt.Errorf("push failed after %d retries: %w", maxRetries, lastErr)
}

// PushAll sends all declarations to the topology API.
// Errors for individual declarations are logged but processing continues.
func (p *HTTPPusher) PushAll(ctx context.Context, declarations []LinkDeclaration) {
	successCount := 0
	errorCount := 0

	for _, decl := range declarations {
		if err := p.Push(ctx, decl); err != nil {
			log.Printf("ERROR: failed to push declaration for node %s: %v", decl.Node.ID, err)
			errorCount++
		} else {
			successCount++
		}
	}

	log.Printf("Push complete: %d succeeded, %d failed out of %d total",
		successCount, errorCount, len(declarations))
}
