use std::collections::HashMap;

pub struct PhoneBook {
    entries: HashMap<String, String>,
}

impl PhoneBook {
    pub fn new() -> Self {
        PhoneBook {
            entries: HashMap::new(),
        }
    }

    pub fn add_entry(&mut self, name: String, number: String) {
        self.entries.insert(name, number);
    }

    pub fn get_entry(&self, name: &str) -> Option<&String> {
        self.entries.get(name)
    }

    pub fn list_entries(&self) -> Vec<(&String, &String)> {
        self.entries.iter().collect()
    }

    pub fn remove_entry(&mut self, name: &str) -> Option<String> {
        self.entries.remove(name)
    }

    pub fn register(&mut self, service: String, fqdn: String) -> Result<String, String> {
        for (existing_service, existing_fqdn) in &self.entries {
            if existing_fqdn == &fqdn && existing_service != &service {
                return Err("FQDN already registered".to_string());
            }
        }

        self.entries.insert(service, fqdn);
        Ok("Registered successfully".to_string())
    }
}

impl Default for PhoneBook {
    fn default() -> Self {
        Self::new()
    }
}
