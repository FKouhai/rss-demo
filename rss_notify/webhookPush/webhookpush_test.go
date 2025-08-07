package webhookpush

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func startMockServerContent() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		// nolint
		w.Write([]byte(`{"feed_url":["https://mockedurl.com/with/new/article"]}`))
	}))
}

func TestGetContent(t *testing.T) {
	mockServer := startMockServerContent()
	defer mockServer.Close()
	var d DiscordNotification
	req, err := http.Get(mockServer.URL)
	if err != nil {
		t.Error(err)
	}

	// nolint
	defer req.Body.Close()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Error(err)
	}
	got, err := d.GetContent(body)
	if err != nil {
		t.Error(err)
	}
	want := []string{"https://mockedurl.com/with/new/article"}
	wantMap := make(map[string]bool)
	for _, v := range want {
		wantMap[v] = true
	}
	for _, v := range got {
		if !wantMap[v] {
			t.Errorf("got %v , want: %v", got, want)
		}

	}
}

func TestGetContentError(t *testing.T) {
	b := []byte("malformatted string")
	var d DiscordNotification
	_, err := d.GetContent(b)
	if err == nil {
		t.Error("got no error when was expecting one")
	}
}

func TestSendNotification(t *testing.T) {
	mockServer := mockReceiverEndpoint(http.StatusNoContent)

	d := DiscordNotification{
		WebHookURL: mockServer.URL,
	}

	msg := []string{"https://www.tomshardware.com/video-games/pc-gaming/signalrgb-takes-a-swipe-at-razer-makes-functioning-rgb-toaster-pc-quad-slice-toaster-case-incorporates-a-stream-deck-mini-itx-components"}
	resp, err := d.SendNotification(msg)
	if err != nil {
		t.Error(err)
	}
	if resp != http.StatusNoContent {
		t.Error("Expected 204 response")
	}
}

func TestSendNotificationError(t *testing.T) {
	d := DiscordNotification{
		WebHookURL: "https://notWorkingUrl.com/",
	}
	msg := []string{"https://mockedurl.com/with/new/article"}

	_, err := d.SendNotification(msg)
	if err == nil {
		t.Error("Expected error but got nil")
	}
}

func mockReceiverEndpoint(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
}
