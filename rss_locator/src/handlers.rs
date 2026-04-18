use actix_web::{get, post, web, HttpResponse, Responder};
use opentelemetry::global;
use opentelemetry::trace::TraceContextExt;
use opentelemetry::KeyValue;
use opentelemetry::trace::Tracer;
use std::sync::{Arc, Mutex};

use crate::models::{ErrorResponse, FqdnResponse, RegisterRequest, ServiceRequest, SuccessResponse};
use crate::phonebook::PhoneBook;
use log;

#[post("/register")]
async fn register_handler(
    req: web::Json<RegisterRequest>,
    phonebook: web::Data<Arc<Mutex<PhoneBook>>>,
) -> impl Responder {
    log::info!("Registration request from service '{}' with FQDN '{}'", req.service, req.fqdn);

    let tracer = global::tracer("locator");
    tracer.in_span("handlers.register_handler", |cx| {
        cx.span().set_attribute(KeyValue::new("service.name", req.service.clone()));
        cx.span().set_attribute(KeyValue::new("service.fqdn", req.fqdn.clone()));

        let mut phonebook = phonebook.lock().unwrap();
        match phonebook.register(req.service.clone(), req.fqdn.clone()) {
            Ok(message) => {
                log::info!("Successfully registered service '{}': {}", req.service, message);
                cx.span().set_attribute(KeyValue::new("http.status_code", 200));
                HttpResponse::Ok().json(SuccessResponse { message })
            }
            Err(error) => {
                log::error!("Registration failed for service '{}': {}", req.service, error);
                cx.span().set_attribute(KeyValue::new("http.status_code", 400));
                cx.span().set_attribute(KeyValue::new("error.message", error.clone()));
                HttpResponse::BadRequest().json(ErrorResponse { error })
            }
        }
    })
}

#[post("/services")]
async fn services_handler(
    req: web::Json<ServiceRequest>,
    phonebook: web::Data<Arc<Mutex<PhoneBook>>>,
) -> impl Responder {
    log::info!("Service discovery request for '{}'", req.service);

    let tracer = global::tracer("locator");
    tracer.in_span("handlers.services_handler", |cx| {
        cx.span().set_attribute(KeyValue::new("service.query", req.service.clone()));

        let phonebook = phonebook.lock().unwrap();
        match phonebook.get_entry(&req.service) {
            Some(fqdn) => {
                log::info!("Found service '{}' at FQDN '{}'", req.service, fqdn);
                cx.span().set_attribute(KeyValue::new("http.status_code", 200));
                cx.span().set_attribute(KeyValue::new("service.fqdn", fqdn.clone()));
                HttpResponse::Ok().json(FqdnResponse { fqdn: fqdn.clone() })
            }
            None => {
                log::warn!("Service '{}' not found in phonebook", req.service);
                cx.span().set_attribute(KeyValue::new("http.status_code", 404));
                HttpResponse::NotFound().json(ErrorResponse {
                    error: "Service not found".to_string(),
                })
            }
        }
    })
}

#[get("/health")]
async fn healthz() -> impl Responder {
    log::debug!("Health check requested");
    let tracer = global::tracer("locator");
    tracer.in_span("handlers.healthz", |cx| {
        cx.span().set_attribute(KeyValue::new("http.status_code", 200));
        "healthy".to_string()
    })
}
