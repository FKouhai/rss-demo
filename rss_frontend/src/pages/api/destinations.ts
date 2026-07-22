import type { APIRoute } from 'astro';
import { lookupService, locatorEndpoint } from '@/lib/locator';

type Destination = {
  id: string;
  name: string;
  type: 'discord' | 'slack' | 'webhook';
  webhook_url: string;
};

let mockDestinations: Destination[] = [
  { id: 'dest-1', name: 'General Discord', type: 'discord', webhook_url: 'https://discord.com/api/webhooks/mock1' },
  { id: 'dest-2', name: 'Alerts Slack', type: 'slack', webhook_url: 'https://hooks.slack.com/services/mock2' },
];

const proxyToConfig = async (path: string, init?: RequestInit): Promise<Response | null> => {
  if (!locatorEndpoint()) return null;
  const fqdn = await lookupService('config');
  if (!fqdn) return null;
  try {
    const res = await fetch(`${fqdn}${path}`, { signal: AbortSignal.timeout(2000), ...init });
    if (res.ok) return res;
  } catch {}
  return null;
};

const json = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });

export const GET: APIRoute = async () => {
  const upstream = await proxyToConfig('/destinations');
  if (upstream) return new Response(await upstream.text(), { headers: { 'Content-Type': 'application/json' } });
  return json({ destinations: mockDestinations });
};

export const POST: APIRoute = async ({ request }) => {
  let body: Partial<Destination>;
  try { body = await request.json(); }
  catch { return json({ error: 'Invalid JSON body' }, 400); }

  const upstream = await proxyToConfig('/destinations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (upstream) return new Response(await upstream.text(), { headers: { 'Content-Type': 'application/json' } });

  const newDest: Destination = {
    id: `dest-${Date.now()}`,
    name: body.name || 'Unnamed',
    type: body.type || 'discord',
    webhook_url: body.webhook_url || '',
  };
  mockDestinations.push(newDest);
  return json({ destinations: mockDestinations });
};

export const DELETE: APIRoute = async ({ request }) => {
  let body: { id?: string };
  try { body = await request.json(); }
  catch { return json({ error: 'Invalid JSON body' }, 400); }

  const upstream = await proxyToConfig('/destinations', {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (upstream) return new Response(await upstream.text(), { headers: { 'Content-Type': 'application/json' } });

  mockDestinations = mockDestinations.filter(d => d.id !== body.id);
  return json({ destinations: mockDestinations });
};
