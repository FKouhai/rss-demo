package methods

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestPushNotificationHandler(t *testing.T) {
	mockServer := mockReceiverEndpoint(http.StatusNoContent)
	data := fmt.Sprintf(`{"feed_url":["https://www.reddit.com/r/sre/comments/1meh785/a_developer_wants_you_to_deploy_their_application/"],"webhook_url":"%v"}`, mockServer.URL)
	payload := []byte(data)
	requestRecorder := httptest.NewRecorder()
	req, err := http.NewRequest("POST", "/push", bytes.NewBuffer(payload))
	if err != nil {
		t.Error(err)
	}
	req.Header.Add("Content-Type", "application/json")
	handler := http.HandlerFunc(PushNotificationHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusNoContent {
		t.Errorf("got %v , was expecting %v", requestRecorder.Code, http.StatusNoContent)
	}
}

func TestPushNotificationHandlerGet(t *testing.T) {
	requestRecorder := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/push", nil)
	if err != nil {
		t.Error(err)
	}
	handler := http.HandlerFunc(PushNotificationHandler)
	handler.ServeHTTP(requestRecorder, req)

	if requestRecorder.Code != http.StatusBadRequest {
		t.Errorf("got %v , was expecting %v", requestRecorder.Code, http.StatusBadRequest)
	}

}

func TestPushNotificationNoFeed(t *testing.T) {
	mockServer := mockReceiverEndpoint(http.StatusBadRequest)
	data := fmt.Sprintf(`{"feed_url":"","webhook_url":"%v"}`, mockServer.URL)
	payload := []byte(data)
	requestRecorder := httptest.NewRecorder()
	req, err := http.NewRequest("POST", "/push", bytes.NewBuffer(payload))
	req.Header.Add("Content-Type", "application/json")
	if err != nil {
		t.Error(err)
	}
	handler := http.HandlerFunc(PushNotificationHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusBadRequest {
		t.Errorf("got %v , was expecting %v", requestRecorder.Code, http.StatusBadRequest)
	}
}

func TestPushNotificationNoDestination(t *testing.T) {
	payload := []byte(`{"feed_url":["https://www.reddit.com/r/sre/comments/1meh785/a_developer_wants_you_to_deploy_their_application/"],"webhook_url":""}`)
	requestRecorder := httptest.NewRecorder()
	req, err := http.NewRequest("POST", "/push", bytes.NewBuffer(payload))
	req.Header.Add("Content-Type", "application/json")
	if err != nil {
		t.Error(err)
	}
	handler := http.HandlerFunc(PushNotificationHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusInternalServerError {
		t.Errorf("got %v , was expecting %v", requestRecorder.Code, http.StatusInternalServerError)
	}
}

func TestPushNotificationNoContentJSON(t *testing.T) {
	payload := []byte(`{""}`)
	requestRecorder := httptest.NewRecorder()
	req, err := http.NewRequest("POST", "/push", bytes.NewBuffer(payload))
	req.Header.Add("Content-Type", "application/json")
	if err != nil {
		t.Error(err)
	}
	handler := http.HandlerFunc(PushNotificationHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusBadRequest {
		t.Errorf("got %v , was expecting %v", requestRecorder.Code, http.StatusBadRequest)
	}
}

func TestPushNotificationNoJSON(t *testing.T) {
	payload := []byte(`{""}`)
	requestRecorder := httptest.NewRecorder()
	req, err := http.NewRequest("POST", "/push", bytes.NewBuffer(payload))
	if err != nil {
		t.Error(err)
	}
	handler := http.HandlerFunc(PushNotificationHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusBadRequest {
		t.Errorf("got %v , was expecting %v", requestRecorder.Code, http.StatusBadRequest)
	}
}

// errorReader struct that implements the io.Reader interface
// used for mocking an "errror" response when reading the body
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("test error")
}

func TestBodyReadError(t *testing.T) {
	req := httptest.NewRequest("POST", "/push", &errorReader{})
	req.Header.Set("Content-Type", "application/json")
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(PushNotificationHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusBadRequest {
		t.Errorf("got %v , was expecting %v", requestRecorder.Code, http.StatusBadRequest)
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

func mockReceiverEndpoint(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
}
