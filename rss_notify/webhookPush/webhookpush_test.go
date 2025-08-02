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
		w.Write([]byte(`{"feed_url":"https://mockedurl.com/with/new/article"}`))
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
	want := "https://mockedurl.com/with/new/article"
	if want != got {
		t.Errorf("got %v , want: %v", got, want)
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

	// TODO mock discord url where the response should be a 204
	d := DiscordNotification{
		WebHookURL: mockServer.URL,
	}

	resp, err := d.SendNotification("https://www.tomshardware.com/video-games/pc-gaming/signalrgb-takes-a-swipe-at-razer-makes-functioning-rgb-toaster-pc-quad-slice-toaster-case-incorporates-a-stream-deck-mini-itx-components")
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

	_, err := d.SendNotification("https://mockedurl.com/with/new/article")
	if err == nil {
		t.Error("Expected error but got nil")
	}
}

func mockReceiverEndpoint(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
}
