import type { APIRoute } from 'astro';
import { lookupService, locatorEndpoint } from '@/lib/locator';

interface ServiceStatus {
  name: string;
  healthy: boolean;
  error?: string;
}

async function checkHealth(name: string, url: string): Promise<ServiceStatus> {
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(5000) });
    return { name, healthy: res.ok };
  } catch (e: any) {
    return { name, healthy: false, error: e?.message ?? 'unreachable' };
  }
}

export const GET: APIRoute = async () => {
  const locator = locatorEndpoint();

  if (!locator) {
    return new Response(
      JSON.stringify([{ name: 'rss-locator', healthy: false, error: 'LOCATOR_URL not configured' }]),
      { headers: { 'Content-Type': 'application/json' } },
    );
  }

  // Look up rss-poller and rss-notify in parallel while also checking the locator directly.
  const [pollerFqdn, notifyFqdn] = await Promise.all([
    lookupService('poller'),
    lookupService('notify'),
  ]);

  const checks: Promise<ServiceStatus>[] = [
    // Locator: we already have its address, check health directly.
    checkHealth('rss-locator', `${locator}/health`),

    // Poller: discovered via locator.
    pollerFqdn
      ? checkHealth('rss-poller', `${pollerFqdn}/healthz`)
      : Promise.resolve({ name: 'rss-poller', healthy: false, error: 'not registered in locator' }),

    // Notify: discovered via locator.
    notifyFqdn
      ? checkHealth('rss-notify', `${notifyFqdn}/healthz`)
      : Promise.resolve({ name: 'rss-notify', healthy: false, error: 'not registered in locator' }),
  ];

  const results = await Promise.all(checks);

  return new Response(JSON.stringify(results), {
    headers: { 'Content-Type': 'application/json' },
  });
};
