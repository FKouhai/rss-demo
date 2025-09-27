// Package methods contains all the http handlers for the config service
package methods

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// HealthHandler handler for health endpoint
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	log.InfoFmt("connection accepted to %v", r.URL.Path)
	w.Write([]byte(`{"status": "healthy"}`))
	w.WriteHeader(http.StatusOK)

}

var (
	errMethod = errors.New("unsupported method")
)

// config service needs to POST to the poller service its configuration
// config service needs to poll the poller service every 5 seconds to check if the config is empty or not
// if the config is empty then the poller service will POST again to a newly spawned pod
// the config service needs to read/write a file (json/yaml) to push the configs

// ConfigHandler Reads the incoming request to set up the config
func ConfigHandler(w http.ResponseWriter, r *http.Request) {
	_, span := instrumentation.GetTracer("config").Start(r.Context(), "handlers.ReadHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	if r.Method != http.MethodPost {
		log.Info(errMethod.Error())
		span.AddEvent(errMethod.Error())
		span.RecordError(errMethod)
		span.SetStatus(codes.Error, errMethod.Error())
	}
	// check if request header has Content-Type/json
	// if its not then return a http.StatusBadRequest

	// instantiate the valkey client
	client := valkeyClient()

	var c ConfigStruct
	// Use the valkey client to try to get the feeds
	data, err := c.GetFeeds(client)

	if err != nil {
		log.Info("Unable to retrieve data from valkey")
		// parse incoming json request and SetFeeds
		data, err = io.ReadAll(r.Body)
		defer r.Body.Close()
		if err != nil {
			log.ErrorFmt("%v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		err = json.Unmarshal(data, &c)
		if err != nil {
			log.ErrorFmt("%v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		err = c.SetFeeds(client)
		if err != nil {
			log.ErrorFmt("%v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		return
	}
	log.Info(string(data))
	err = c.PushConfig(data)
	if err != nil {
		log.ErrorFmt("%v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
