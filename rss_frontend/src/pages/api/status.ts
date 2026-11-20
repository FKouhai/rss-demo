import type { APIRoute } from 'astro';
import { pollerUrl, notifyUrl, outboxUrl } from '@/lib/services';

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
  // Health is checked directly per service — no registry involved.
  const results = await Promise.all([
    checkHealth('rss-poller', `${pollerUrl()}/healthz`),
    checkHealth('rss-notify', `${notifyUrl()}/healthz`),
    checkHealth('rss-outbox', `${outboxUrl()}/healthz`),
  ]);

  return new Response(JSON.stringify(results), {
    headers: { 'Content-Type': 'application/json' },
  });
};
