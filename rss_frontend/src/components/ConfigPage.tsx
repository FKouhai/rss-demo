import { useState, useEffect, useRef } from 'preact/hooks';
import { Trash2, Plus, Check, Loader2, Globe, Slack, MessageSquare } from 'lucide-preact';

type Feed = {
  id: string;
  url: string;
  interval: string;
  enabled: boolean;
  notification_target_id: string | null;
};

type NotificationTarget = {
  id: string;
  name: string;
  type: 'discord' | 'slack' | 'webhook';
  webhook_url: string;
};

type RoutingConfig = {
  global_default: string | null;
  overrides: Record<string, string>;
};

const INTERVALS = ['30s', '5m', '15m', '30m', '1h', '6h'];

const destIcon = (type: string) => {
  if (type === 'slack') return <Slack className="h-4 w-4" />;
  if (type === 'discord') return <MessageSquare className="h-4 w-4" />;
  return <Globe className="h-4 w-4" />;
};

export const ConfigPage = () => {
  const [activeTab, setActiveTab] = useState<'feeds' | 'destinations' | 'routing'>('feeds');

  const [feeds, setFeeds] = useState<Feed[]>([]);
  const [feedsLoading, setFeedsLoading] = useState(true);
  const [feedInput, setFeedInput] = useState('');
  const [intervalInput, setIntervalInput] = useState('30m');
  const [feedsSaving, setFeedsSaving] = useState(false);

  const [destinations, setDestinations] = useState<NotificationTarget[]>([]);
  const [destLoading, setDestLoading] = useState(true);
  const [destSaving, setDestSaving] = useState(false);
  const [destNameInput, setDestNameInput] = useState('');
  const [destTypeInput, setDestTypeInput] = useState<'discord' | 'slack' | 'webhook'>('discord');
  const [destUrlInput, setDestUrlInput] = useState('');

  const [globalDefault, setGlobalDefault] = useState<string>('');
  const [overrides, setOverrides] = useState<Record<string, string>>({});
  const [routingSaving, setRoutingSaving] = useState(false);

  const [status, setStatus] = useState<{ ok: boolean; message: string } | null>(null);
  const statusTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const showStatus = (ok: boolean, message: string) => {
    if (statusTimer.current) clearTimeout(statusTimer.current);
    setStatus({ ok, message });
    statusTimer.current = setTimeout(() => setStatus(null), 4000);
  };

  useEffect(() => {
    fetch('/api/feeds/config')
      .then(r => r.json())
      .then((data: { feeds?: Feed[] }) => {
        if (data.feeds) setFeeds(data.feeds);
      })
      .catch(() => {})
      .finally(() => setFeedsLoading(false));

    fetch('/api/destinations')
      .then(r => r.json())
      .then((data: { destinations?: NotificationTarget[] }) => {
        if (data.destinations) setDestinations(data.destinations);
      })
      .catch(() => {})
      .finally(() => setDestLoading(false));

    fetch('/api/routing')
      .then(r => r.json())
      .then((data: Partial<RoutingConfig>) => {
        setGlobalDefault(data.global_default || '');
        if (data.overrides) setOverrides(data.overrides);
      })
      .catch(() => {});
  }, []);

  const addFeed = () => {
    const url = feedInput.trim();
    if (!url) return;
    if (feeds.some(f => f.url === url)) {
      showStatus(false, 'Feed already in list.');
      return;
    }
    try { new URL(url); } catch {
      showStatus(false, 'Invalid URL.');
      return;
    }
    setFeeds(prev => [...prev, {
      id: `pending-${Date.now()}`,
      url,
      interval: intervalInput,
      enabled: true,
      notification_target_id: null,
    }]);
    setFeedInput('');
  };

  const removeFeed = (id: string) => {
    setFeeds(prev => prev.filter(f => f.id !== id));
    setOverrides(prev => {
      const next = { ...prev };
      delete next[id];
      return next;
    });
  };

  const toggleFeedEnabled = (id: string) =>
    setFeeds(prev => prev.map(f => f.id === id ? { ...f, enabled: !f.enabled } : f));

  const saveFeeds = async () => {
    setFeedsSaving(true);
    try {
      const res = await fetch('/api/feeds/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ feeds }),
      });
      res.ok ? showStatus(true, 'Feeds saved.') : showStatus(false, 'Failed to save feeds.');
    } catch {
      showStatus(false, 'Network error.');
    } finally {
      setFeedsSaving(false);
    }
  };

  const createDestination = async (e: Event) => {
    e.preventDefault();
    const name = destNameInput.trim();
    const webhook_url = destUrlInput.trim();
    if (!name || !webhook_url) { showStatus(false, 'Name and URL required.'); return; }
    setDestSaving(true);
    try {
      const res = await fetch('/api/destinations', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, type: destTypeInput, webhook_url }),
      });
      if (res.ok) {
        const data = await res.json();
        setDestinations(data.destinations || []);
        setDestNameInput('');
        setDestUrlInput('');
        showStatus(true, 'Destination added.');
      } else {
        showStatus(false, 'Failed to add destination.');
      }
    } catch {
      showStatus(false, 'Network error.');
    } finally {
      setDestSaving(false);
    }
  };

  const deleteDestination = async (id: string) => {
    try {
      const res = await fetch('/api/destinations', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id }),
      });
      if (res.ok) {
        const data = await res.json();
        setDestinations(data.destinations || []);
        if (globalDefault === id) setGlobalDefault('');
        setOverrides(prev => {
          const next = { ...prev };
          Object.keys(next).forEach(k => { if (next[k] === id) delete next[k]; });
          return next;
        });
        showStatus(true, 'Destination removed.');
      }
    } catch {}
  };

  const saveRouting = async () => {
    setRoutingSaving(true);
    try {
      const res = await fetch('/api/routing', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ global_default: globalDefault || null, overrides }),
      });
      res.ok ? showStatus(true, 'Routing saved.') : showStatus(false, 'Failed to save routing.');
    } catch {
      showStatus(false, 'Network error.');
    } finally {
      setRoutingSaving(false);
    }
  };

  return (
    <div className="w-full p-6 max-w-3xl mx-auto">
      {status && (
        <div className={`fixed top-4 right-4 z-50 rounded-lg border px-4 py-3 text-sm font-medium shadow-lg animate-slide-in ${
          status.ok
            ? 'border-green-200 bg-green-50 text-green-800 dark:border-green-800 dark:bg-green-950 dark:text-green-200'
            : 'border-red-200 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-950 dark:text-red-200'
        }`}>
          {status.message}
        </div>
      )}

      <div className="flex gap-1 border-b border-border/50 mb-6">
        {(['feeds', 'destinations', 'routing'] as const).map(tab => (
          <button
            key={tab}
            onClick={() => setActiveTab(tab)}
            className={`px-4 py-2.5 text-sm font-medium transition-colors capitalize border-b-2 -mb-px ${
              activeTab === tab
                ? 'border-primary text-foreground'
                : 'border-transparent text-muted-foreground hover:text-foreground'
            }`}
          >
            {tab}
          </button>
        ))}
      </div>

      {activeTab === 'feeds' && (
        <div className="space-y-4">
          <div className="flex gap-2">
            <input
              type="url"
              placeholder="https://example.com/feed.xml"
              value={feedInput}
              onInput={e => setFeedInput((e.target as HTMLInputElement).value)}
              onKeyDown={e => e.key === 'Enter' && addFeed()}
              className="flex-1 rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
            <select
              value={intervalInput}
              onChange={e => setIntervalInput((e.target as HTMLSelectElement).value)}
              className="rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {INTERVALS.map(i => <option key={i} value={i}>{i}</option>)}
            </select>
            <button
              onClick={addFeed}
              className="rounded-md bg-primary text-primary-foreground px-4 py-2 text-sm font-medium hover:bg-primary/90 transition-colors flex items-center gap-1.5"
            >
              <Plus className="h-4 w-4" /> Add
            </button>
          </div>

          {feedsLoading ? (
            <div className="py-8 text-center text-sm text-muted-foreground flex items-center justify-center gap-2">
              <Loader2 className="h-4 w-4 animate-spin" /> Loading...
            </div>
          ) : feeds.length === 0 ? (
            <div className="py-8 text-center text-sm text-muted-foreground border border-dashed border-border/60 rounded-md">
              No feeds configured.
            </div>
          ) : (
            <div className="divide-y divide-border/40 border border-border/40 rounded-md overflow-hidden">
              {feeds.map(feed => (
                <div key={feed.id} className="flex items-center gap-3 px-4 py-3 bg-background/50 hover:bg-accent/10 transition-colors">
                  <button
                    onClick={() => toggleFeedEnabled(feed.id)}
                    className={`shrink-0 relative h-5 w-9 rounded-full transition-colors ${feed.enabled ? 'bg-primary' : 'bg-input'}`}
                    title={feed.enabled ? 'Disable' : 'Enable'}
                  >
                    <span className={`absolute top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${feed.enabled ? 'translate-x-4' : 'translate-x-0.5'}`} />
                  </button>
                  <span className="flex-1 text-sm truncate">{feed.url}</span>
                  <span className="text-xs text-muted-foreground shrink-0 tabular-nums">{feed.interval}</span>
                  <button
                    onClick={() => removeFeed(feed.id)}
                    className="shrink-0 text-muted-foreground hover:text-destructive p-1 rounded transition-colors"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              ))}
            </div>
          )}

          {feeds.length > 0 && (
            <button
              onClick={saveFeeds}
              disabled={feedsSaving}
              className="w-full rounded-md bg-primary text-primary-foreground px-4 py-2.5 text-sm font-medium hover:bg-primary/90 transition-colors flex items-center justify-center gap-2 disabled:opacity-50"
            >
              {feedsSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
              Save
            </button>
          )}
        </div>
      )}

      {activeTab === 'destinations' && (
        <div className="space-y-6">
          <form onSubmit={createDestination} className="border border-border/40 rounded-md p-4 space-y-3 bg-background/50">
            <p className="text-sm font-medium">Add destination</p>
            <div className="grid grid-cols-3 gap-3">
              <input
                type="text"
                placeholder="Name"
                value={destNameInput}
                onInput={e => setDestNameInput((e.target as HTMLInputElement).value)}
                className="rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
              <select
                value={destTypeInput}
                onChange={e => setDestTypeInput((e.target as HTMLSelectElement).value as 'discord' | 'slack' | 'webhook')}
                className="rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <option value="discord">Discord</option>
                <option value="slack">Slack</option>
                <option value="webhook">Webhook</option>
              </select>
              <input
                type="url"
                placeholder="Webhook URL"
                value={destUrlInput}
                onInput={e => setDestUrlInput((e.target as HTMLInputElement).value)}
                className="rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            </div>
            <button
              type="submit"
              disabled={destSaving}
              className="w-full rounded-md bg-primary text-primary-foreground px-4 py-2 text-sm font-medium hover:bg-primary/90 transition-colors flex items-center justify-center gap-2 disabled:opacity-50"
            >
              {destSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
              Add
            </button>
          </form>

          {destLoading ? (
            <div className="py-8 text-center text-sm text-muted-foreground flex items-center justify-center gap-2">
              <Loader2 className="h-4 w-4 animate-spin" /> Loading...
            </div>
          ) : destinations.length === 0 ? (
            <div className="py-8 text-center text-sm text-muted-foreground border border-dashed border-border/60 rounded-md">
              No destinations configured.
            </div>
          ) : (
            <div className="divide-y divide-border/40 border border-border/40 rounded-md overflow-hidden">
              {destinations.map(dest => (
                <div key={dest.id} className="flex items-center gap-3 px-4 py-3 bg-background/50 hover:bg-accent/10 transition-colors">
                  <span className="shrink-0 text-muted-foreground">{destIcon(dest.type)}</span>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium truncate">{dest.name}</p>
                    <p className="text-xs text-muted-foreground truncate">{dest.webhook_url}</p>
                  </div>
                  <span className="text-xs text-muted-foreground capitalize shrink-0">{dest.type}</span>
                  <button
                    onClick={() => deleteDestination(dest.id)}
                    className="shrink-0 text-muted-foreground hover:text-destructive p-1 rounded transition-colors"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {activeTab === 'routing' && (
        <div className="space-y-6">
          <div className="border border-border/40 rounded-md p-4 space-y-3 bg-background/50">
            <div>
              <p className="text-sm font-medium">Global default</p>
              <p className="text-xs text-muted-foreground mt-0.5">Feeds without an override send to this target.</p>
            </div>
            <select
              value={globalDefault}
              onChange={e => setGlobalDefault((e.target as HTMLSelectElement).value)}
              className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <option value="">None</option>
              {destinations.map(d => (
                <option key={d.id} value={d.id}>{d.name}</option>
              ))}
            </select>
          </div>

          {feeds.length > 0 && destinations.length > 0 && (
            <div className="space-y-2">
              <p className="text-sm font-medium">Per-feed overrides</p>
              <div className="divide-y divide-border/40 border border-border/40 rounded-md overflow-hidden">
                {feeds.map(feed => (
                  <div key={feed.id} className="flex items-center gap-4 px-4 py-3 bg-background/50">
                    <span className="flex-1 text-sm text-muted-foreground truncate">{feed.url}</span>
                    <select
                      value={overrides[feed.id] || ''}
                      onChange={e => {
                        const val = (e.target as HTMLSelectElement).value;
                        setOverrides(prev => {
                          const next = { ...prev };
                          if (val) next[feed.id] = val;
                          else delete next[feed.id];
                          return next;
                        });
                      }}
                      className="shrink-0 rounded-md border border-input bg-background px-3 py-1.5 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    >
                      <option value="">Use global default</option>
                      {destinations.map(d => (
                        <option key={d.id} value={d.id}>{d.name}</option>
                      ))}
                    </select>
                  </div>
                ))}
              </div>
            </div>
          )}

          {feeds.length === 0 && (
            <div className="py-6 text-center text-sm text-muted-foreground border border-dashed border-border/60 rounded-md">
              Configure feeds first to set per-feed overrides.
            </div>
          )}

          {destinations.length === 0 && (
            <div className="py-6 text-center text-sm text-muted-foreground border border-dashed border-border/60 rounded-md">
              Add destinations first to configure routing.
            </div>
          )}

          <button
            onClick={saveRouting}
            disabled={routingSaving}
            className="w-full rounded-md bg-primary text-primary-foreground px-4 py-2.5 text-sm font-medium hover:bg-primary/90 transition-colors flex items-center justify-center gap-2 disabled:opacity-50"
          >
            {routingSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
            Save routing
          </button>
        </div>
      )}
    </div>
  );
};

export default ConfigPage;
