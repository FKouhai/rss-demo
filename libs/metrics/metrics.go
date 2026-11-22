package metrics

import (
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Namespace default prometheus namespace
const Namespace string = "rss"

// Registry service agnostic struct for creating new prometheus registries
type Registry struct {
	reg     *prometheus.Registry
	service string
}

// Init constructor for metrics registry
func Init(service string) *Registry {
	service = strings.ReplaceAll(service, "-", "_")
	reg := prometheus.NewRegistry()
	return &Registry{
		reg:     reg,
		service: service,
	}
}

// Counter new counter for a given registry
func (r *Registry) Counter(name, help string, labels ...string) *prometheus.CounterVec {
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace:   Namespace,
		Subsystem:   r.service,
		Name:        name,
		Help:        help,
		ConstLabels: prometheus.Labels{"service": r.service},
	}, labels)
	r.reg.MustRegister(cv)
	return cv
}

// Gauge new gauge vector for a given registry
func (r *Registry) Gauge(name, help string, labels ...string) *prometheus.GaugeVec {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace:   Namespace,
		Subsystem:   r.service,
		Name:        name,
		Help:        help,
		ConstLabels: prometheus.Labels{"service": r.service},
	}, labels)
	r.reg.MustRegister(g)

	return g
}

// Histogram new histogram vector for a given registry
func (r *Registry) Histogram(name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
	b := buckets
	if b == nil {
		b = prometheus.DefBuckets
	}

	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   Namespace,
		Subsystem:   r.service,
		Name:        name,
		Help:        help,
		Buckets:     b,
		ConstLabels: prometheus.Labels{"service": r.service},
	}, labels)
	r.reg.MustRegister(h)

	return h
}

// Handler thin wrapper for a particular service prom handler
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{})

}
