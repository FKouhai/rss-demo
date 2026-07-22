import { useState, useEffect } from 'preact/hooks';
import { Server, Cpu, Bell, Activity, RefreshCw, AlertCircle, ShieldAlert } from 'lucide-preact';

const REFRESH_MS = 30 * 1000; // 30 seconds

interface ServiceStatus {
  name: string;
  healthy: boolean;
  error?: string;
}

const ServiceIcon = ({ name }: { name: string }) => {
  if (name.includes('locator')) return <Server className="h-5 w-5 text-blue-500" />;
  if (name.includes('poller')) return <Cpu className="h-5 w-5 text-orange-500" />;
  if (name.includes('notify')) return <Bell className="h-5 w-5 text-green-500" />;
  return <Activity className="h-5 w-5 text-gray-500" />;
};

const StatusBadge = ({ healthy }: { healthy: boolean }) => (
  <span
    className={`inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-semibold ${
      healthy
        ? 'bg-green-500/10 text-green-600 dark:text-green-400'
        : 'bg-destructive/10 text-destructive'
    }`}
  >
    <span
      className={`h-1.5 w-1.5 rounded-full ${healthy ? 'bg-green-500 animate-pulse' : 'bg-destructive'}`}
    />
    {healthy ? 'Operational' : 'Degraded'}
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
    <div className="w-full p-4 min-h-screen flex flex-col items-center">
      <div className="w-full max-w-2xl mx-auto px-4">
        {/* Header */}
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 mb-8">
          <div>
            <h1 className="text-4xl font-bold">
              System Status
            </h1>
            <p className="text-sm text-muted-foreground pt-1">
              Health status of your RSS services.
            </p>
          </div>
          {lastChecked && (
            <span className="text-xs text-muted-foreground bg-accent/40 backdrop-blur-sm px-3 py-1.5 rounded-full border border-border/20 self-start md:self-auto">
              Updated: {lastChecked.toLocaleTimeString()}
            </span>
          )}
        </div>

        {/* System Banner */}
        {!loading && (
          <div className={`mb-6 rounded-xl border p-4 text-sm font-semibold flex items-center gap-3 backdrop-blur-sm shadow-sm ${
            allHealthy
              ? 'border-green-200/50 bg-green-50/50 text-green-800 dark:border-green-800/40 dark:bg-green-950/20 dark:text-green-300'
              : 'border-destructive/20 bg-destructive/10 text-destructive'
          }`}>
            {allHealthy ? (
              <>
                <div className="h-2 w-2 rounded-full bg-green-500 animate-ping" />
                <span>All microservices are operational. Connections healthy.</span>
              </>
            ) : (
              <>
                <ShieldAlert className="h-5 w-5 shrink-0" />
                <span>One or more microservices report health anomalies.</span>
              </>
            )}
          </div>
        )}

        {/* Services Status Cards List */}
        <div className="flex flex-col gap-4">
          {loading
            ? Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="h-16 rounded-xl border border-border/30 animate-pulse bg-muted/30" />
              ))
            : services.map(service => (
                <div
                  key={service.name}
                  className="glass-card flex items-center justify-between rounded-xl px-5 py-4 border border-border/40"
                >
                  <div className="flex items-center gap-4">
                    <div className="p-2.5 rounded-lg bg-accent/50 border border-border/20 shrink-0">
                      <ServiceIcon name={service.name} />
                    </div>
                    <div className="truncate">
                      <p className="font-bold text-foreground capitalize">
                        {service.name.replace('-', ' ')}
                      </p>
                      {service.error ? (
                        <p className="text-xs text-destructive flex items-center gap-1 mt-0.5 font-medium">
                          <AlertCircle className="h-3 w-3 shrink-0" /> {service.error}
                        </p>
                      ) : (
                        <p className="text-[10px] uppercase font-bold tracking-wider text-muted-foreground mt-0.5">
                          Active node
                        </p>
                      )}
                    </div>
                  </div>
                  <StatusBadge healthy={service.healthy} />
                </div>
              ))
          }
        </div>

        {/* Action controls */}
        <div className="mt-6 flex justify-end">
          <button
            onClick={fetchStatus}
            disabled={loading}
            className="text-xs font-semibold text-muted-foreground hover:text-foreground transition-colors flex items-center gap-1.5 bg-accent/40 hover:bg-accent/80 border border-border/20 px-4 py-2 rounded-lg"
          >
            <RefreshCw className={`h-3 w-3 ${loading ? 'animate-spin' : ''}`} />
            Refresh status
          </button>
        </div>
      </div>
    </div>
  );
};

export default Status;
