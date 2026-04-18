use actix_web::{web, App, HttpServer};
use rss_locator::phonebook::PhoneBook;
use rss_locator::telemetry;
use std::sync::{Arc, Mutex};

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    env_logger::init_from_env(env_logger::Env::new().default_filter_or("info"));

    // Initialise telemetry.  On failure we log and continue — the service is
    // still fully functional, just without traces.
    let tracer_provider = match telemetry::init_tracer("locator") {
        Ok(tp) => {
            eprintln!("Telemetry initialised successfully");
            Some(tp)
        }
        Err(e) => {
            eprintln!("Failed to initialize tracer: {}. Running without telemetry.", e);
            None
        }
    };

    let phonebook = Arc::new(Mutex::new(PhoneBook::new()));
    HttpServer::new(move || {
        let phonebook = phonebook.clone();
        App::new()
            .app_data(web::Data::new(phonebook.clone()))
            .wrap(opentelemetry_instrumentation_actix_web::RequestTracing::new())
            .service(rss_locator::handlers::register_handler)
            .service(rss_locator::handlers::services_handler)
            .service(rss_locator::handlers::healthz)
    })
    .bind(("0.0.0.0", 3000))?
    .run()
    .await
    .map_err(|e| {
        eprintln!("Server error: {}", e);
        e
    })?;

    // Flush and shut down the tracer provider so buffered spans reach the
    // collector before the process exits.
    if let Some(tp) = tracer_provider {
        if let Err(e) = tp.shutdown() {
            eprintln!("TracerProvider shutdown error: {}", e);
        }
    }

    Ok(())
}
