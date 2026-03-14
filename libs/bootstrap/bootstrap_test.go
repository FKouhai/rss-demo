package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInit(t *testing.T) {
	tests := []struct {
		name         string
		serviceName  string
		fqdn         string
		locatorURL   string
		mockResponse int
		mockBody     string
		wantErr      bool
	}{
		{
			name:         "successful registration",
			serviceName:  "test-service",
			fqdn:         "test-service.local",
			locatorURL:   "",
			mockResponse: 200,
			mockBody:     `{"message":"service registered"}`,
			wantErr:      false,
		},
		{
			name:         "registration failed with 400",
			serviceName:  "test-service",
			fqdn:         "test-service.local",
			locatorURL:   "",
			mockResponse: 400,
			mockBody:     `{"error":"service already registered"}`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.mockResponse)
				w.Write([]byte(tt.mockBody))
			}))
			defer server.Close()

			t.Setenv("LOCATOR_URL", server.URL)

			err := Init(tt.serviceName, tt.fqdn)
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInitNoLocatorURL(t *testing.T) {
	t.Setenv("LOCATOR_URL", "")

	err := Init("test-service", "test-service.local")
	if err != nil {
		t.Errorf("Init() should not error when LOCATOR_URL is not set, got: %v", err)
	}
}
