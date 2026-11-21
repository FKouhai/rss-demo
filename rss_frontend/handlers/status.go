package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/FKouhai/rss-demo/libs/auth"
	"github.com/FKouhai/rss-frontend/views"
)

// userLabel returns the display name for the masthead, "" when open mode.
func userLabel(r *http.Request) string {
	id, ok := auth.FromContext(r.Context())
	if !ok || id.User == "" {
		return id.Email
	}
	return id.User
}

// ServiceStatus is one row of the status dashboard. It lives in views
// so templates and handlers share it without an import cycle.
type ServiceStatus = views.ServiceStatus

func (d Deps) checkOne(name, base string) ServiceStatus {
	st := ServiceStatus{Name: name}
	client := d.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	// Bound the fan-out so one hung backend cannot hang the page.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	resp, err := client.Do(req)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	defer func() { _ = resp.Body.Close() }()
	st.Healthy = resp.StatusCode == http.StatusOK
	if !st.Healthy {
		st.Error = resp.Status
	}
	return st
}

func (d Deps) probeAll() []ServiceStatus {
	return []ServiceStatus{
		d.checkOne("rss-poller", d.PollerURL),
		d.checkOne("rss-notify", d.NotifyURL),
		d.checkOne("rss-outbox", d.OutboxURL),
	}
}

func (d Deps) statusHandler(w http.ResponseWriter, r *http.Request) {
	st := d.probeAll()
	renderErr(views.Status(views.StatusParams{
		Title: "Status", User: userLabel(r), AuthEnabled: d.AuthEnabled,
		Services: st, Updated: time.Now(),
	}).Render(r.Context(), w))
}

func (d Deps) statusPartial(w http.ResponseWriter, r *http.Request) {
	renderErr(views.StatusTable(d.probeAll()).Render(r.Context(), w))
}
