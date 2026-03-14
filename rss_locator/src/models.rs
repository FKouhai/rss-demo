use serde::{Deserialize, Serialize};

#[derive(Debug, Serialize, Deserialize)]
pub struct RegisterRequest {
    pub service: String,
    pub fqdn: String,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct ServiceRequest {
    pub service: String,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct FqdnResponse {
    pub fqdn: String,
}

#[derive(Debug, Serialize)]
pub struct ErrorResponse {
    pub error: String,
}

#[derive(Debug, Serialize)]
pub struct SuccessResponse {
    pub message: String,
}
