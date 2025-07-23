package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMain(t *testing.T) {
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(RootHandler)
	handler.ServeHTTP(requestRecorder, req)
	status := requestRecorder.Code
	if status != http.StatusOK {
		t.Errorf("Handler returned a different code from 200: %v", status)
	}
	want := "testing 1 2"
	got := requestRecorder.Body.String()
	if got != want {
		t.Errorf("The server returned an unexpected body: got %v, want: %v", got, want)
	}
}

func TestConfig(t *testing.T) {
	payload := []byte(`{"rss_feeds": "https://example.com/rss"}`)
	req, err := http.NewRequest("POST", "/config", bytes.NewBuffer(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(ConfigHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusOK {
		t.Errorf("Handler returned a different code from 200: %v", requestRecorder.Code)
	}
	want := "https://example.com/rss"
	got := requestRecorder.Body.String()
	if got != want {
		t.Errorf("The server returned an unexpected body: got %v, want: %v", got, want)
	}
}

func TestConfigNotGet(t *testing.T) {
	req, err := http.NewRequest("GET", "/config", nil)
	if err != nil {
		t.Fatal(err)
	}
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(ConfigHandler)
	handler.ServeHTTP(requestRecorder, req)
	want := http.StatusBadRequest
	got := requestRecorder.Code
	if got != want {
		t.Errorf("Want: %v, Got: %v", want, got)
	}
}

func TestHealth(t *testing.T) {
	req, err := http.NewRequest("GET", "/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(HealthzHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusOK {
		t.Fatal(err)
	}
	var response map[string]string
	want := map[string]string{"status": "healthy"}
	err = json.NewDecoder(requestRecorder.Body).Decode(&response)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(want, response) {
		t.Errorf("Expected %v, got: %v", want, response)
	}
}
