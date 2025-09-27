{ ... }:
{
  project.name = "local_infra";
  services.valkey.service = {
    image = "valkey/valkey-bundle:8.1.0-alpine";
    ports = [
      "6379:6379"
    ];
  };
  services.jaeger.service = {
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
  services.rss_notify.service = {
    image = "rss_notify:latest";
    ports = [
      "3001:3000"
    ];
    environment = {
      OTEL_EP = "jaeger:4317";
    };
  };
  services.rss_poller.service = {
    image = "rss_poller:latest";
    ports = [
      "3000:3000"
    ];
    environment = {
      OTEL_EP = "jaeger:4317";
      NOTIFICATION_ENDPOINT = "http://rss_notify:3000/push";
      NOTIFICATION_SENDER = "http://discord_webhook_placeholder";
    };
  };
  services.rss_frontend.service = {
    image = "rss_frontend:latest";
    ports = [
      "4321:4321"
    ];
    environment = {
      POLLER_ENDPOINT = "http://rss_poller:3000/rss";
    };
  };
}
