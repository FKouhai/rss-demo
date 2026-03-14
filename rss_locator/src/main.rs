use actix_web::{web, App, HttpServer};
use rss_locator::phonebook::PhoneBook;
use rss_locator::telemetry;
use std::sync::{Arc, Mutex};

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    env_logger::init_from_env(env_logger::Env::new().default_filter_or("info"));

    if let Err(e) = telemetry::init_tracer("locator") {
        eprintln!("Failed to initialize tracer: {}. Continuing without telemetry.", e);
        return Err(std::io::Error::new(
            std::io::ErrorKind::Other,
            "Failed to initialize tracer",
        ));
    }

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

    //sleep(Duration::from_secs(2)).await;
    eprintln!("Tracer provider shutdown skipped");
    Ok(())
}
