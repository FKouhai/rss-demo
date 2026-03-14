use opentelemetry::global;
use opentelemetry::KeyValue;
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_sdk::resource::Resource;
use opentelemetry_sdk::trace::{Sampler, SdkTracerProvider};
use std::env;
use std::error::Error;

pub fn init_tracer(service_name: &str) -> Result<(), Box<dyn Error + Send + Sync + 'static>> {
    let otel_ep = env::var("OTEL_EP").unwrap_or_else(|_| {
        eprintln!("OTEL_EP not set, using default localhost:4317");
        "localhost:4317".to_string()
    });

    eprintln!("using OTEL_EP={}", otel_ep);

    let resource = Resource::builder_empty()
        .with_attribute(KeyValue::new("service.name", format!("{}", service_name)))
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

    global::set_tracer_provider(tracer_provider);

    Ok(())
}
