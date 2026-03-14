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
