import type { APIRoute } from 'astro';
import { configUrl } from '@/lib/services';

type RoutingConfig = {
  global_default: string | null;
  overrides: Record<string, string>;
};

let mockRouting: RoutingConfig = {
  global_default: 'dest-1',
  overrides: {},
};

const proxyToConfig = async (path: string, init?: RequestInit): Promise<Response | null> => {
  // No config service yet — direct DNS fails fast and callers fall back to mocks.
  try {
    const res = await fetch(`${configUrl()}${path}`, { signal: AbortSignal.timeout(2000), ...init });
    if (res.ok) return res;
  } catch {}
  return null;
};

const json = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });

export const GET: APIRoute = async () => {
  const upstream = await proxyToConfig('/routing');
  if (upstream) return new Response(await upstream.text(), { headers: { 'Content-Type': 'application/json' } });
  return json(mockRouting);
};

export const POST: APIRoute = async ({ request }) => {
  let body: Partial<RoutingConfig>;
  try { body = await request.json(); }
  catch { return json({ error: 'Invalid JSON body' }, 400); }

  const upstream = await proxyToConfig('/routing', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (upstream) return new Response(await upstream.text(), { headers: { 'Content-Type': 'application/json' } });

  mockRouting = {
    global_default: body.global_default ?? mockRouting.global_default,
    overrides: body.overrides ?? mockRouting.overrides,
  };
  return json({ ok: true, ...mockRouting });
};
