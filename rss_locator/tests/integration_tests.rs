use actix_web::{test, web, App};

use rss_locator::handlers;
use rss_locator::models::{FqdnResponse, RegisterRequest, ServiceRequest};
use rss_locator::phonebook::PhoneBook;
use std::sync::{Arc, Mutex};

#[actix_web::test]
async fn test_register_new_service() {
    let phonebook = Arc::new(Mutex::new(PhoneBook::new()));
    let app = test::init_service(
        App::new()
            .app_data(web::Data::new(phonebook.clone()))
            .service(handlers::register_handler),
    )
    .await;

    let req = test::TestRequest::post()
        .uri("/register")
        .set_json(&RegisterRequest {
            service: "config".to_string(),
            fqdn: "config.demo.kubernetes.service.local".to_string(),
        })
        .to_request();

    let resp = test::call_service(&app, req).await;
    assert!(resp.status().is_success());
}

#[actix_web::test]
async fn test_update_existing_service() {
    let phonebook = Arc::new(Mutex::new(PhoneBook::new()));
    phonebook
        .lock()
        .unwrap()
        .register("config".to_string(), "config.old.fqdn".to_string())
        .unwrap();

    let app = test::init_service(
        App::new()
            .app_data(web::Data::new(phonebook.clone()))
            .service(handlers::register_handler),
    )
    .await;

    let req = test::TestRequest::post()
        .uri("/register")
        .set_json(&RegisterRequest {
            service: "config".to_string(),
            fqdn: "config.new.fqdn".to_string(),
        })
        .to_request();

    let resp = test::call_service(&app, req).await;
    assert!(resp.status().is_success());

    let phonebook = phonebook.lock().unwrap();
    assert_eq!(
        phonebook.get_entry("config"),
        Some(&"config.new.fqdn".to_string())
    );
}

#[actix_web::test]
async fn test_register_duplicate() {
    let phonebook = Arc::new(Mutex::new(PhoneBook::new()));
    phonebook
        .lock()
        .unwrap()
        .register("config".to_string(), "config.demo.kubernetes.service.local".to_string())
        .unwrap();

    let app = test::init_service(
        App::new()
            .app_data(web::Data::new(phonebook.clone()))
            .service(handlers::register_handler),
    )
    .await;

    let req = test::TestRequest::post()
        .uri("/register")
        .set_json(&RegisterRequest {
            service: "config".to_string(),
            fqdn: "config.demo.kubernetes.service.local".to_string(),
        })
        .to_request();

    let resp = test::call_service(&app, req).await;
    assert_eq!(resp.status(), 400);
}

#[actix_web::test]
async fn test_register_fqdn_conflict() {
    let phonebook = Arc::new(Mutex::new(PhoneBook::new()));
    phonebook
        .lock()
        .unwrap()
        .register("config".to_string(), "shared.fqdn".to_string())
        .unwrap();

    let app = test::init_service(
        App::new()
            .app_data(web::Data::new(phonebook.clone()))
            .service(handlers::register_handler),
    )
    .await;

    let req = test::TestRequest::post()
        .uri("/register")
        .set_json(&RegisterRequest {
            service: "notify".to_string(),
            fqdn: "shared.fqdn".to_string(),
        })
        .to_request();

    let resp = test::call_service(&app, req).await;
    assert_eq!(resp.status(), 400);
}

#[actix_web::test]
async fn test_services_lookup_success() {
    let phonebook = Arc::new(Mutex::new(PhoneBook::new()));
    phonebook
        .lock()
        .unwrap()
        .register("config".to_string(), "config.demo.kubernetes.service.local".to_string())
        .unwrap();

    let app = test::init_service(
        App::new()
            .app_data(web::Data::new(phonebook.clone()))
            .service(handlers::services_handler),
    )
    .await;

    let req = test::TestRequest::get()
        .uri("/services")
        .set_json(&ServiceRequest {
            service: "config".to_string(),
        })
        .to_request();

    let resp: FqdnResponse = test::call_and_read_body_json(&app, req).await;
    assert_eq!(resp.fqdn, "config.demo.kubernetes.service.local");
}

#[actix_web::test]
async fn test_services_lookup_not_found() {
    let phonebook = Arc::new(Mutex::new(PhoneBook::new()));

    let app = test::init_service(
        App::new()
            .app_data(web::Data::new(phonebook.clone()))
            .service(handlers::services_handler),
    )
    .await;

    let req = test::TestRequest::get()
        .uri("/services")
        .set_json(&ServiceRequest {
            service: "nonexistent".to_string(),
        })
        .to_request();

    let resp = test::call_service(&app, req).await;
    assert_eq!(resp.status(), 404);
}

#[actix_web::test]
async fn test_full_workflow() {
    let phonebook = Arc::new(Mutex::new(PhoneBook::new()));
    let app = test::init_service(
        App::new()
            .app_data(web::Data::new(phonebook.clone()))
            .service(handlers::register_handler)
            .service(handlers::services_handler),
    )
    .await;

    let register_req = test::TestRequest::post()
        .uri("/register")
        .set_json(&RegisterRequest {
            service: "config".to_string(),
            fqdn: "config.demo.kubernetes.service.local".to_string(),
        })
        .to_request();

    let resp = test::call_service(&app, register_req).await;
    assert!(resp.status().is_success());

    let lookup_req = test::TestRequest::get()
        .uri("/services")
        .set_json(&ServiceRequest {
            service: "config".to_string(),
        })
        .to_request();

    let resp: FqdnResponse = test::call_and_read_body_json(&app, lookup_req).await;
    assert_eq!(resp.fqdn, "config.demo.kubernetes.service.local");
}
