'use client';

import { useEffect, useState } from 'react';
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts';
import {
  mockMetrics,
  mockChartData,
  mockInterceptionEvents,
} from '@/lib/mockData';

interface ProxyStats {
  total_capital_saved_zar: number;
  savings_growth_text: string;
  cache_hit_rate: number;
  proxy_status: string;
  memory_usage_mb: number;
  active_connections: number;
  recent_interceptions: InterceptionEvent[];
}

interface InterceptionEvent {
  timestamp: string;
  target_endpoint: string;
  action: string;
  latency_ms: number;
  ttl_remaining: string;
}

interface ChartDataPoint {
  date: string;
  totalRequests: number;
  cachedRequests: number;
}

export default function HomePage() {
  const [stats, setStats] = useState<ProxyStats | null>(null);
  const [chartData, setChartData] = useState<ChartDataPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchMetrics = async () => {
      try {
        const res = await fetch('http://localhost:8081/metrics');
        if (!res.ok) {
          throw new Error(`API error: ${res.status}`);
        }
        const data: ProxyStats = await res.json();
        setStats(data);
        setError(null);
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : 'Unknown error';
        console.warn(`Failed to fetch telemetry: ${errorMessage}. Using mock data.`);
        setError(errorMessage);
      } finally {
        setLoading(false);
      }
    };

    const fetchHistory = async () => {
      try {
        const res = await fetch('http://localhost:8081/api/metrics/history');
        if (res.ok) {
          const data = await res.json();
          setChartData(data);
        }
      } catch (err) {
        console.warn('Failed to fetch history, falling back to mock.');
      }
    };

    fetchMetrics();
    fetchHistory();

    const interval = setInterval(() => {
      fetchMetrics();
      fetchHistory();
    }, 5000);

    return () => clearInterval(interval);
  }, []);

  const formatRand = (value: number) => {
    return new Intl.NumberFormat('en-ZA', {
      style: 'currency',
      currency: 'ZAR',
      minimumFractionDigits: 0,
      maximumFractionDigits: 0,
    }).format(value);
  };

  // Use live stats if available, otherwise fall back to mock data
  const displayStats = stats || {
    total_capital_saved_zar: mockMetrics.totalSavedZAR,
    savings_growth_text: `+${mockMetrics.monthlyGrowthPercent}% this month`,
    cache_hit_rate: mockMetrics.cacheHitRate,
    proxy_status: mockMetrics.proxyStatus,
    memory_usage_mb: mockMetrics.ramUsageMB,
    active_connections: mockMetrics.activeConnections,
    recent_interceptions: mockInterceptionEvents.map((e) => ({
      timestamp: e.timestamp,
      target_endpoint: e.endpoint,
      action: e.action,
      latency_ms: e.latencyMs,
      ttl_remaining: e.ttlRemaining,
    })),
  };

  const isOnline = displayStats.proxy_status?.toLowerCase() === 'online';
  const statusDot = isOnline ? 'bg-green-500' : 'bg-red-500';

  // Fallback chart data
  const displayChartData = chartData.length > 0 ? chartData : mockChartData;

  return (
    <main className="min-h-screen w-full flex-1 p-8 transition-colors duration-200 relative z-10">
      <div className="mx-auto max-w-7xl space-y-8">
        {/* Header Banner */}
        <section className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-8 md:p-10 shadow-xl shadow-slate-200/50 dark:shadow-none backdrop-blur-xl">
          <p className="text-xs uppercase tracking-[0.3em] font-semibold text-emerald-600 dark:text-emerald-400">
            Mintry Fabric Control Plane
          </p>
          <h1 className="mt-4 text-3xl md:text-4xl font-bold tracking-tight text-slate-900 dark:text-white">
            Operational Cost & Policy Overview
          </h1>
          <p className="mt-4 max-w-3xl text-base text-slate-500 dark:text-slate-350 leading-relaxed">
            Real-time analytics for your zero-touch TLS proxy. Monitor caching efficiency, API costs saved, and database performance in one visual workspace.
          </p>
          {error && !stats && (
            <p className="mt-4 text-xs font-semibold text-amber-600 dark:text-amber-400 flex items-center gap-1.5">
              <span>⚠️</span> Telemetry connection unreachable. Displaying demo metrics.
            </p>
          )}
          {stats && !error && (
            <p className="mt-4 text-xs font-semibold text-emerald-600 dark:text-emerald-400 flex items-center gap-1.5">
              <span>✓</span> Connected to active telemetry node.
            </p>
          )}
        </section>

        {/* Hero Metrics Row */}
        <section className="grid gap-6 md:grid-cols-3">
          {/* Total Capital Saved */}
          <article className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-8 shadow-xl shadow-slate-200/30 dark:shadow-none transition-all hover:scale-[1.01]">
            <p className="text-xs font-semibold uppercase tracking-[0.2em] text-emerald-600 dark:text-emerald-400">
              Total Capital Saved (ZAR)
            </p>
            <div className="mt-4 flex items-baseline gap-3">
              <p className="text-3xl font-bold text-slate-900 dark:text-white">
                {formatRand(displayStats.total_capital_saved_zar)}
              </p>
              <span className="inline-flex items-center gap-1 rounded-full bg-emerald-100 dark:bg-emerald-900/50 px-2.5 py-0.5 text-xs font-bold text-emerald-700 dark:text-emerald-300">
                <span>↑</span>
                {displayStats.savings_growth_text}
              </span>
            </div>
            <p className="mt-4 text-xs text-slate-500 dark:text-slate-400 leading-normal">
              Accumulative budget savings generated by blocking duplicate vendor calls.
            </p>
          </article>

          {/* Global Cache Hit Rate */}
          <article className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-8 shadow-xl shadow-slate-200/30 dark:shadow-none transition-all hover:scale-[1.01]">
            <p className="text-xs font-semibold uppercase tracking-[0.2em] text-blue-600 dark:text-blue-400">
              Cache Hit Rate
            </p>
            <p className="mt-4 text-3xl font-bold text-slate-900 dark:text-white">
              {displayStats.cache_hit_rate.toFixed(1)}%
            </p>
            <p className="mt-4 text-xs text-slate-500 dark:text-slate-400 leading-normal">
              Percentage of total requests resolved locally without accessing vendor APIs.
            </p>
            <div className="mt-6 h-1.5 w-full rounded-full bg-slate-200 dark:bg-slate-850">
              <div
                className="h-full rounded-full bg-gradient-to-r from-blue-500 to-teal-400 transition-all duration-300"
                style={{ width: `${Math.min(displayStats.cache_hit_rate, 100)}%` }}
              ></div>
            </div>
          </article>

          {/* Proxy Health */}
          <article className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-8 shadow-xl shadow-slate-200/30 dark:shadow-none transition-all hover:scale-[1.01]">
            <p className="text-xs font-semibold uppercase tracking-[0.2em] text-cyan-600 dark:text-cyan-400">
              Proxy Status
            </p>
            <div className="mt-4 flex items-center gap-2">
              <span className={`inline-block h-2.5 w-2.5 rounded-full ${statusDot} animate-pulse`}></span>
              <p className={`text-xl font-bold ${isOnline ? 'text-green-600 dark:text-green-400' : 'text-red-500'}`}>
                {displayStats.proxy_status}
              </p>
            </div>
            <p className="mt-4 text-xs text-slate-500 dark:text-slate-400 leading-normal">
              {displayStats.memory_usage_mb.toFixed(1)}MB RAM usage · {displayStats.active_connections} active threads
            </p>
            <p className="mt-4 text-[10px] font-mono uppercase tracking-wider text-slate-400 dark:text-slate-500">
              LibSQLCipher Engine Active
            </p>
          </article>
        </section>

        {/* Time-Series Chart */}
        <section className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-8 shadow-xl shadow-slate-200/30 dark:shadow-none">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between mb-6 gap-3">
            <div>
              <h2 className="text-xl font-bold text-slate-900 dark:text-white">
                Request Volume Trend
              </h2>
              <p className="text-xs text-slate-500 dark:text-slate-400 mt-1">
                Visualizing daily cache savings vs total microservice demand.
              </p>
            </div>
            <div className="flex items-center gap-4 text-xs">
              <div className="flex items-center gap-1.5">
                <span className="h-3 w-3 rounded bg-slate-400 dark:bg-slate-600 inline-block"></span>
                <span className="text-slate-600 dark:text-slate-350">Outbound Requests</span>
              </div>
              <div className="flex items-center gap-1.5">
                <span className="h-3 w-3 rounded bg-emerald-500 inline-block"></span>
                <span className="text-slate-600 dark:text-slate-350">Cache Hits (Savings)</span>
              </div>
            </div>
          </div>
          <div className="h-80 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={displayChartData}>
                <defs>
                  <linearGradient id="colorTotal" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#0066FF" stopOpacity={0.4} />
                    <stop offset="95%" stopColor="#0066FF" stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="colorCached" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#00E5A3" stopOpacity={0.4} />
                    <stop offset="95%" stopColor="#00E5A3" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#e2e8f0" className="dark:hidden" />
                <CartesianGrid strokeDasharray="3 3" stroke="#334155" className="hidden dark:block" />
                <XAxis
                  dataKey="date"
                  stroke="#94a3b8"
                  style={{ fontSize: '11px', fontWeight: 600 }}
                  dy={10}
                />
                <YAxis
                  stroke="#94a3b8"
                  style={{ fontSize: '11px', fontWeight: 600 }}
                  dx={-10}
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: 'var(--tw-background-color, #1e293b)',
                    border: '1px solid #e2e8f0',
                    borderRadius: '12px',
                    fontSize: '12px',
                  }}
                />
                <Area
                  type="monotone"
                  dataKey="totalRequests"
                  stroke="#0066FF"
                  strokeWidth={2}
                  fillOpacity={1}
                  fill="url(#colorTotal)"
                  name="Total Requests"
                  isAnimationActive={false}
                />
                <Area
                  type="monotone"
                  dataKey="cachedRequests"
                  stroke="#00E5A3"
                  strokeWidth={2}
                  fillOpacity={1}
                  fill="url(#colorCached)"
                  name="Cached Requests"
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </section>

        {/* Live Interception Preview */}
        <section className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-8 shadow-xl shadow-slate-200/30 dark:shadow-none">
          <div className="flex items-center justify-between mb-6">
            <div>
              <h2 className="text-xl font-bold text-slate-900 dark:text-white">
                Recent Interceptions
              </h2>
              <p className="text-xs text-slate-500 dark:text-slate-400 mt-1">
                A sliding window of recent TLS decryptions and cash logs.
              </p>
            </div>
            <a
              href="/feed"
              className="text-xs font-semibold text-emerald-600 dark:text-emerald-400 hover:underline"
            >
              View Full Feed →
            </a>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-slate-200 dark:border-slate-800 text-slate-500 dark:text-slate-400">
                  <th className="px-4 py-3.5 text-left font-bold uppercase tracking-wider">
                    Timestamp
                  </th>
                  <th className="px-4 py-3.5 text-left font-bold uppercase tracking-wider">
                    Target Endpoint
                  </th>
                  <th className="px-4 py-3.5 text-center font-bold uppercase tracking-wider">
                    Action
                  </th>
                  <th className="px-4 py-3.5 text-right font-bold uppercase tracking-wider">
                    Latency
                  </th>
                  <th className="px-4 py-3.5 text-right font-bold uppercase tracking-wider">
                    TTL
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800/60">
                {displayStats.recent_interceptions.slice(0, 8).map((event, idx) => (
                  <tr
                    key={idx}
                    className="hover:bg-slate-100/50 dark:hover:bg-slate-800/40 transition-colors"
                  >
                    <td className="px-4 py-3.5 font-mono text-slate-555 dark:text-slate-400">
                      {typeof event.timestamp === 'string'
                        ? event.timestamp.split('T')[1]?.substring(0, 8) || event.timestamp
                        : new Date(event.timestamp).toLocaleTimeString()}
                    </td>
                    <td className="px-4 py-3.5 font-medium text-slate-700 dark:text-slate-300">
                      {event.target_endpoint}
                    </td>
                    <td className="px-4 py-3.5 text-center">
                      {event.action === 'CACHE_HIT' ? (
                        <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 dark:bg-emerald-950/40 px-2.5 py-0.5 text-[10px] font-bold text-emerald-600 dark:text-emerald-400 border border-emerald-200/30 dark:border-emerald-800/20">
                          <span className="h-1.5 w-1.5 rounded-full bg-emerald-500"></span>
                          CACHE HIT
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1.5 rounded-full bg-slate-100 dark:bg-slate-800/50 px-2.5 py-0.5 text-[10px] font-bold text-slate-600 dark:text-slate-450 border border-slate-200 dark:border-slate-800/30">
                          <span className="h-1.5 w-1.5 rounded-full bg-slate-400 dark:bg-slate-500"></span>
                          VENDOR CALL
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3.5 text-right font-mono font-medium text-slate-600 dark:text-slate-300">
                      {event.latency_ms.toFixed(2)}ms
                    </td>
                    <td className="px-4 py-3.5 text-right font-mono text-slate-500 dark:text-slate-450">
                      {event.ttl_remaining}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </main>
  );
}
