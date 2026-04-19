'use strict';
const { diag, DiagConsoleLogger, DiagLogLevel } = require('@opentelemetry/api');
const { NodeSDK } = require('@opentelemetry/sdk-node');
const { getNodeAutoInstrumentations } = require('@opentelemetry/auto-instrumentations-node');
const { OTLPTraceExporter } = require('@opentelemetry/exporter-trace-otlp-grpc');
const { resourceFromAttributes } = require('@opentelemetry/resources');
const { ATTR_SERVICE_NAME, ATTR_SERVICE_VERSION } = require('@opentelemetry/semantic-conventions');
const grpc = require('@grpc/grpc-js');
const { version } = require('./package.json');

diag.setLogger(new DiagConsoleLogger(), DiagLogLevel.DEBUG);

// OTEL_EP is "host:port" (e.g. "jaeger:4317"), matching the convention used by
// the other services in this stack. gRPC exporter expects plain host:port.
const ep = process.env.OTEL_EP;

const sdk = new NodeSDK({
  resource: resourceFromAttributes({
    [ATTR_SERVICE_NAME]: 'rss-frontend',
    [ATTR_SERVICE_VERSION]: version,
  }),
  traceExporter: new OTLPTraceExporter({
    url: ep ?? 'localhost:4317',
    credentials: grpc.credentials.createInsecure(),
  }),
  instrumentations: [
    getNodeAutoInstrumentations({
      // Disable noisy fs instrumentation — traces for every file read on startup
      // create a lot of noise without actionable signal.
      '@opentelemetry/instrumentation-fs': { enabled: false },
    }),
  ],
});

sdk.start();

process.on('SIGTERM', () => {
  sdk.shutdown().finally(() => process.exit(0));
});
