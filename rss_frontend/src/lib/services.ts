// Service endpoints without a registry.
//
// In-cluster DNS names (rss_poller, rss_notify, ...) work out of the box.
// Each endpoint can be overridden with an env var for local development
// outside the cluster (e.g. POLLER_URL=http://localhost:3000).
function endpoint(envVar: string, dnsName: string, port = 3000): string {
  const fromEnv = process.env[envVar];
  if (fromEnv) return fromEnv.replace(/\/+$/, '');
  return `http://${dnsName}:${port}`;
}

export const pollerUrl = () => endpoint('POLLER_URL', 'rss_poller');
export const notifyUrl = () => endpoint('NOTIFY_URL', 'rss_notify');
export const outboxUrl = () => endpoint('OUTBOX_URL', 'rss_outbox');
// No config service yet — destinations/routing fall back to mocks until it lands.
export const configUrl = () => endpoint('CONFIG_URL', 'rss_config');
