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
    image = "jaegertracing/all-in-one:1.71.0";
    environment = {
      COLLECTOR_OTLP_ENABLED = "true";
    };
    ports = [
      "4317:4317"
      "4318:4318"
      "16686:16686"
    ];
  };
}
