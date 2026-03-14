use crate::phonebook::PhoneBook;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new_phonebook() {
        let pb = PhoneBook::new();
        assert_eq!(pb.get_entry("test"), None);
    }

    #[test]
    fn test_register_new_service() {
        let mut pb = PhoneBook::new();
        let result = pb.register("config".to_string(), "config.demo.kubernetes.service.local".to_string());
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), "Registered successfully");
        assert_eq!(
            pb.get_entry("config"),
            Some(&"config.demo.kubernetes.service.local".to_string())
        );
    }

    #[test]
    fn test_register_duplicate() {
        let mut pb = PhoneBook::new();
        pb.register("config".to_string(), "config.demo.kubernetes.service.local".to_string())
            .unwrap();
        let result = pb.register("config".to_string(), "config.demo.kubernetes.service.local".to_string());
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "Service already registered with this FQDN");
    }

    #[test]
    fn test_register_fqdn_conflict() {
        let mut pb = PhoneBook::new();
        pb.register("config".to_string(), "shared.fqdn".to_string()).unwrap();
        let result = pb.register("notify".to_string(), "shared.fqdn".to_string());
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "FQDN already registered");
    }

    #[test]
    fn test_register_update_existing() {
        let mut pb = PhoneBook::new();
        pb.register("config".to_string(), "config.old.fqdn".to_string())
            .unwrap();
        let result = pb.register("config".to_string(), "config.new.fqdn".to_string());
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), "Updated existing service");
        assert_eq!(
            pb.get_entry("config"),
            Some(&"config.new.fqdn".to_string())
        );
    }

    #[test]
    fn test_add_entry() {
        let mut pb = PhoneBook::new();
        pb.add_entry("config".to_string(), "config.demo.kubernetes.service.local".to_string());
        assert_eq!(
            pb.get_entry("config"),
            Some(&"config.demo.kubernetes.service.local".to_string())
        );
    }

    #[test]
    fn test_remove_entry() {
        let mut pb = PhoneBook::new();
        pb.add_entry("config".to_string(), "config.demo.kubernetes.service.local".to_string());
        let result = pb.remove_entry("config");
        assert_eq!(result, Some("config.demo.kubernetes.service.local".to_string()));
        assert_eq!(pb.get_entry("config"), None);
    }

    #[test]
    fn test_list_entries() {
        let mut pb = PhoneBook::new();
        pb.add_entry("config".to_string(), "config.demo.kubernetes.service.local".to_string());
        pb.add_entry("notify".to_string(), "notify.demo.kubernetes.service.local".to_string());
        let entries = pb.list_entries();
        assert_eq!(entries.len(), 2);
    }

    #[test]
    fn test_get_number_not_found() {
        let pb = PhoneBook::new();
        assert_eq!(pb.get_entry("nonexistent"), None);
    }
}
