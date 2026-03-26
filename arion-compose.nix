_: {
  project.name = "local_infra";
  services = {
    jaeger.service = {
      image = "jaegertracing/all-in-one:1.73.0";
      environment = {
        COLLECTOR_OTLP_ENABLED = "true";
      };
      ports = [
        "4317:4317"
        "4318:4318"
        "16686:16686"
      ];
    };

    rss_notify.service = {
      image = "rss_notify:latest";
      ports = [
        "3001:3000"
      ];
      environment = {
        OTEL_EP = "jaeger:4317";
        LOCATOR_URL = "http://rss_locator:3000";
        SERVICE_FQDN = "rss_notify:3000";
      };
    };

    rss_poller.service = {
      image = "rss_poller:latest";
      ports = [
        "3000:3000"
      ];
      volumes = [
        "./rss_poller/config.json:/etc/rss-poller/config.json:ro"
      ];
      environment = {
        OTEL_EP = "jaeger:4317";
        NOTIFICATION_ENDPOINT = "https://discord.com/api/webhooks/1421594472923267084/207qADiqkjML0Vllr8SX9kF0hgN3piPRxx8pb4tcODcgn-W8VoIVNELfWo7-rTkPlj99";
        LOCATOR_URL = "http://rss_locator:3000";
        SERVICE_FQDN = "rss_poller:3000";
      };
    };

    rss_frontend.service = {
      image = "rss_frontend:latest";
      ports = [
        "4321:4321"
      ];
      environment = {
        OTEL_EP = "jaeger:4317";
        LOCATOR_URL = "http://rss_locator:3000";
      };
    };

    rss_locator.service = {
      image = "rss_locator:latest";
      ports = [
        "3002:3000"
      ];
      environment = {
        OTEL_EP = "jaeger:4317";
      };
    };

  };
}
