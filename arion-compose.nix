{ ... }:
{
  project.name = "local_infra";
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
