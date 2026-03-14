// Package bootstrap provides service registration with the locator service
package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

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

// Init registers the service with the locator service
func Init(serviceName, fqdn string) error {
	locatorURL := os.Getenv("LOCATOR_URL")
	if locatorURL == "" {
		log.Info("LOCATOR_URL not set, skipping locator registration")
		return nil
	}

	registerEndpoint := fmt.Sprintf("%s/register", locatorURL)
	body, err := json.Marshal(registerRequest{Service: serviceName, FQDN: fqdn})
	if err != nil {
		log.ErrorFmt("Failed to marshal register request: %v", err)
		return err
	}

	resp, err := http.Post(registerEndpoint, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.ErrorFmt("Failed to register with locator: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		log.ErrorFmt("Locator registration failed with status %d: %s", resp.StatusCode, errResp["error"])
		return fmt.Errorf("locator registration failed: %s", errResp["error"])
	}

	var successResp registerResponse
	if err := json.NewDecoder(resp.Body).Decode(&successResp); err == nil {
		log.InfoFmt("Successfully registered with locator: %s", successResp.Message)
	} else {
		log.Info("Successfully registered with locator")
	}

	return nil
}

// GetServiceFQDN queries the locator service to discover a service's FQDN
func GetServiceFQDN(serviceName string) (string, error) {
	locatorURL := os.Getenv("LOCATOR_URL")
	if locatorURL == "" {
		return "", fmt.Errorf("LOCATOR_URL not set")
	}

	servicesEndpoint := fmt.Sprintf("%s/services", locatorURL)
	body, err := json.Marshal(serviceRequest{Service: serviceName})
	if err != nil {
		return "", fmt.Errorf("failed to marshal service request: %w", err)
	}

	resp, err := http.Post(servicesEndpoint, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to query locator: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("service '%s' not found in locator", serviceName)
	}

	if resp.StatusCode >= 400 {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		return "", fmt.Errorf("locator query failed: %s", errResp["error"])
	}

	var fqdnResp fqdnResponse
	if err := json.NewDecoder(resp.Body).Decode(&fqdnResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return fqdnResp.FQDN, nil
}
