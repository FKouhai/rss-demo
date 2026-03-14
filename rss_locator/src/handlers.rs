use actix_web::{get, post, web, HttpResponse, Responder};
use opentelemetry::global;
use opentelemetry::trace::TraceContextExt;
use opentelemetry::KeyValue;
use opentelemetry::trace::Tracer;
use std::sync::{Arc, Mutex};

use crate::models::{ErrorResponse, FqdnResponse, RegisterRequest, ServiceRequest, SuccessResponse};
use crate::phonebook::PhoneBook;

#[post("/register")]
async fn register_handler(
    req: web::Json<RegisterRequest>,
    phonebook: web::Data<Arc<Mutex<PhoneBook>>>,
) -> impl Responder {
    let tracer = global::tracer("locator");
    tracer.in_span("handlers.register_handler", |cx| {
        let mut phonebook = phonebook.lock().unwrap();
        match phonebook.register(req.service.clone(), req.fqdn.clone()) {
            Ok(message) => {
                cx.span().set_attribute(KeyValue::new("http.status_code", 200));
                HttpResponse::Ok().json(SuccessResponse { message })
            }
            Err(error) => {
                cx.span().set_attribute(KeyValue::new("http.status_code", 400));
                HttpResponse::BadRequest().json(ErrorResponse { error })
            }
        }
    })
}

#[get("/services")]
async fn services_handler(
    req: web::Json<ServiceRequest>,
    phonebook: web::Data<Arc<Mutex<PhoneBook>>>,
) -> impl Responder {
    let tracer = global::tracer("locator");
    tracer.in_span("handlers.services_handler", |cx| {
        let phonebook = phonebook.lock().unwrap();
        match phonebook.get_entry(&req.service) {
            Some(fqdn) => {
                cx.span().set_attribute(KeyValue::new("http.status_code", 200));
                HttpResponse::Ok().json(FqdnResponse { fqdn: fqdn.clone() })
            }
            None => {
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
    let tracer = global::tracer("locator");
    tracer.in_span("handlers.healthz", |cx| {
        cx.span().set_attribute(KeyValue::new("http.status_code", 200));
        format!("healthy")
    })
}
