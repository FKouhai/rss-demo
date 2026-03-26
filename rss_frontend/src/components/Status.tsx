import { useState, useEffect } from 'preact/hooks';

const REFRESH_MS = 30 * 1000; // 30 seconds

interface ServiceStatus {
  name: string;
  healthy: boolean;
  error?: string;
}

const StatusBadge = ({ healthy }: { healthy: boolean }) => (
  <span
    className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ${
      healthy
        ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
        : 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
    }`}
  >
    <span
      className={`h-1.5 w-1.5 rounded-full ${healthy ? 'bg-green-500' : 'bg-red-500'}`}
    />
    {healthy ? 'Healthy' : 'Unhealthy'}
  </span>
);

const Status = () => {
  const [services, setServices] = useState<ServiceStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [lastChecked, setLastChecked] = useState<Date | null>(null);

  const fetchStatus = async () => {
    try {
      const res = await fetch('/api/status', { headers: { 'Accept': 'application/json' } });
      if (!res.ok) return;
      const data: ServiceStatus[] = await res.json();
      setServices(data);
      setLastChecked(new Date());
    } catch {}
    finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchStatus();
    const id = setInterval(fetchStatus, REFRESH_MS);
    return () => clearInterval(id);
  }, []);

  const allHealthy = services.length > 0 && services.every(s => s.healthy);

  return (
    <div className="p-4 container mx-auto max-w-2xl">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-3xl md:text-4xl font-bold">Service Status</h1>
        {lastChecked && (
          <span className="text-sm text-muted-foreground">
            Last checked {lastChecked.toLocaleTimeString()}
          </span>
        )}
      </div>

      {!loading && (
        <div className={`mb-6 rounded-lg border p-4 text-sm font-medium ${
          allHealthy
            ? 'border-green-200 bg-green-50 text-green-800 dark:border-green-800 dark:bg-green-950 dark:text-green-200'
            : 'border-red-200 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-950 dark:text-red-200'
        }`}>
          {allHealthy ? 'All systems operational' : 'One or more services are unavailable'}
        </div>
      )}

      <div className="flex flex-col gap-3">
        {loading
          ? Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="h-16 rounded-lg border animate-pulse bg-muted" />
            ))
          : services.map(service => (
              <div
                key={service.name}
                className="flex items-center justify-between rounded-lg border px-4 py-3"
              >
                <div>
                  <p className="font-medium">{service.name}</p>
                  {service.error && (
                    <p className="text-xs text-muted-foreground mt-0.5">{service.error}</p>
                  )}
                </div>
                <StatusBadge healthy={service.healthy} />
              </div>
            ))
        }
      </div>

      <div className="mt-4 flex justify-end">
        <button
          onClick={fetchStatus}
          className="text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          Refresh now
        </button>
      </div>
    </div>
  );
};

export default Status;
