// Package bootstrap provides service registration with the locator service
package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	log "github.com/FKouhai/rss-demo/libs/logger"
)

type registerRequest struct {
	Service string `json:"service"`
	FQDN    string `json:"fqdn"`
}

type registerResponse struct {
	Message string `json:"message"`
}

type serviceRequest struct {
	Service string `json:"service"`
}

type fqdnResponse struct {
	FQDN string `json:"fqdn"`
}

type retryConfig struct {
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func retryWithBackoff(name string, config retryConfig, fn func() error) error {
	for attempt := 1; attempt <= config.maxRetries; attempt++ {
		if err := fn(); err != nil {
			if attempt < config.maxRetries {
				delay := minDuration(time.Duration(attempt)*config.baseDelay, config.maxDelay)
				log.InfoFmt("%s: Attempt %d/%d failed. Retrying in %v...", name, attempt, config.maxRetries, delay)
				time.Sleep(delay)
				continue
			}
			log.ErrorFmt("%s: All %d attempts failed", name, config.maxRetries)
			return err
		}
		log.InfoFmt("%s: Success on attempt %d", name, attempt)
		return nil
	}
	return nil
}

// Init registers the service with the locator service
func Init(serviceName, fqdn string) error {
	locatorURL := os.Getenv("LOCATOR_URL")
	if locatorURL == "" {
		log.Info("LOCATOR_URL not set, skipping locator registration")
		return nil
	}

	return retryWithBackoff("Service Registration", retryConfig{
		maxRetries: 3,
		baseDelay:  500 * time.Millisecond,
		maxDelay:   2 * time.Second,
	}, func() error {
		registerEndpoint := fmt.Sprintf("%s/register", locatorURL)
		body, err := json.Marshal(registerRequest{Service: serviceName, FQDN: fqdn})
		if err != nil {
			return fmt.Errorf("failed to marshal register request: %w", err)
		}

		resp, err := http.Post(registerEndpoint, "application/json", bytes.NewBuffer(body))
		if err != nil {
			return fmt.Errorf("failed to register with locator: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			var errResp map[string]string
			json.NewDecoder(resp.Body).Decode(&errResp)
			return fmt.Errorf("locator registration failed with status %d: %s", resp.StatusCode, errResp["error"])
		}

		var successResp registerResponse
		if err := json.NewDecoder(resp.Body).Decode(&successResp); err == nil {
			log.InfoFmt("Successfully registered with locator: %s", successResp.Message)
		} else {
			log.Info("Successfully registered with locator")
		}
		return nil
	})
}

// GetServiceFQDN queries the locator service to discover a service's FQDN
func GetServiceFQDN(serviceName string) (string, error) {
	locatorURL := os.Getenv("LOCATOR_URL")
	if locatorURL == "" {
		return "", fmt.Errorf("LOCATOR_URL not set")
	}

	var result string
	err := retryWithBackoff("Service Discovery", retryConfig{
		maxRetries: 3,
		baseDelay:  500 * time.Millisecond,
		maxDelay:   2 * time.Second,
	}, func() error {
		servicesEndpoint := fmt.Sprintf("%s/services", locatorURL)
		body, err := json.Marshal(serviceRequest{Service: serviceName})
		if err != nil {
			return fmt.Errorf("failed to marshal service request: %w", err)
		}

		resp, err := http.Post(servicesEndpoint, "application/json", bytes.NewBuffer(body))
		if err != nil {
			return fmt.Errorf("failed to query locator: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("service '%s' not found in locator", serviceName)
		}

		if resp.StatusCode >= 400 {
			var errResp map[string]string
			json.NewDecoder(resp.Body).Decode(&errResp)
			return fmt.Errorf("locator query failed: %s", errResp["error"])
		}

		var fqdnResp fqdnResponse
		if err := json.NewDecoder(resp.Body).Decode(&fqdnResp); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}

		result = fqdnResp.FQDN
		return nil
	})

	if err != nil {
		return "", err
	}
	return result, nil
}
