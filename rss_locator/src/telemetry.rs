use opentelemetry::global;
use opentelemetry::KeyValue;
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_sdk::propagation::TraceContextPropagator;
use opentelemetry_sdk::resource::Resource;
use opentelemetry_sdk::trace::{Sampler, SdkTracerProvider};
use std::env;
use std::error::Error;

/// Initialises an OTLP gRPC trace exporter and registers it as the global
/// tracer provider.  Returns the provider so the caller can shut it down
/// cleanly when the process exits.
pub fn init_tracer(
    service_name: &str,
) -> Result<SdkTracerProvider, Box<dyn Error + Send + Sync + 'static>> {
    let otel_ep = env::var("OTEL_EP").unwrap_or_else(|_| {
        eprintln!("OTEL_EP not set, using default localhost:4317");
        "localhost:4317".to_string()
    });

    eprintln!("using OTEL_EP={}", otel_ep);

    let resource = Resource::builder_empty()
        .with_attribute(KeyValue::new("service.name", service_name.to_string()))
        .with_attribute(KeyValue::new("library.language", "rust"))
        .with_attribute(KeyValue::new(
            "service.version",
            env::var("SERVICE_VERSION").unwrap_or_else(|_| "0.1.0".to_string()),
        ))
        .with_attribute(KeyValue::new(
            "deployment.environment",
            env::var("ENV").unwrap_or_else(|_| "development".to_string()),
        ))
        .build();

    let exporter = opentelemetry_otlp::SpanExporter::builder()
        .with_tonic()
        .with_endpoint(format!("http://{}", otel_ep))
        .build()?;

    let tracer_provider = SdkTracerProvider::builder()
        .with_batch_exporter(exporter)
        .with_sampler(Sampler::AlwaysOn)
        .with_resource(resource)
        .build();

    // Keep a clone so the caller can shut it down; the global holds the other ref.
    global::set_tracer_provider(tracer_provider.clone());
    global::set_text_map_propagator(TraceContextPropagator::new());

    Ok(tracer_provider)
}
