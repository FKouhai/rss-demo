package webhookpush

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendNotification(t *testing.T) {
	mockServer := mockReceiverEndpoint(http.StatusNoContent)

	d := DiscordNotification{
		WebHookURL: mockServer.URL,
	}

	msg := []string{"https://www.tomshardware.com/video-games/pc-gaming/signalrgb-takes-a-swipe-at-razer-makes-functioning-rgb-toaster-pc-quad-slice-toaster-case-incorporates-a-stream-deck-mini-itx-components"}
	resp, err := d.SendNotification(context.Background(), msg)
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

	_, err := d.SendNotification(context.Background(), msg)
	if err == nil {
		t.Error("Expected error but got nil")
	}
}

func mockReceiverEndpoint(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
}
