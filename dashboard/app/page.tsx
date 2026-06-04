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

export default function HomePage() {
  const [stats, setStats] = useState<ProxyStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchMetrics = async () => {
      try {
        const res = await fetch('http://localhost:8081/metrics', {
          method: 'GET',
          headers: { 'Content-Type': 'application/json' },
        });

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

    // Fetch immediately on mount
    fetchMetrics();

    // Poll every 5 seconds
    const interval = setInterval(fetchMetrics, 5000);
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

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-8">
      <div className="mx-auto max-w-7xl space-y-8">
        {/* Header */}
        <section className="rounded-3xl border border-slate-800 bg-slate-900/90 p-10 shadow-2xl shadow-slate-950/30 backdrop-blur-xl">
          <p className="text-sm uppercase tracking-[0.3em] text-emerald-300">
            Mintry Fabric
          </p>
          <h1 className="mt-4 text-5xl font-semibold leading-tight text-white">
            Fintech Cost & Security Proxy
          </h1>
          <p className="mt-6 max-w-3xl text-lg leading-8 text-slate-300">
            Real-time visibility into your operational savings. Monitor cache
            hit rates, vendor API costs, and proxy health in one place.
          </p>
          {error && !stats && (
            <p className="mt-4 text-sm text-slate-400 italic">
              📡 Running in demo mode (telemetry unavailable - showing mock data)
            </p>
          )}
          {stats && !error && (
            <p className="mt-4 text-sm text-emerald-400 italic">
              ✓ Connected to live telemetry endpoint
            </p>
          )}
        </section>

        {/* Hero Metrics Row */}
        <section className="grid gap-6 md:grid-cols-3">
          {/* Total Capital Saved */}
          <article className="rounded-3xl border border-slate-800 bg-gradient-to-br from-emerald-900/20 to-slate-900/95 p-8 shadow-xl shadow-slate-950/20">
            <p className="text-sm uppercase tracking-[0.2em] text-emerald-400">
              Total Capital Saved (ZAR)
            </p>
            <div className="mt-4 flex items-baseline gap-3">
              <p className="text-4xl font-bold text-emerald-300">
                {formatRand(displayStats.total_capital_saved_zar)}
              </p>
              <span className="inline-flex items-center gap-1 rounded-full bg-emerald-900/50 px-3 py-1 text-sm font-semibold text-emerald-300">
                <span className="text-emerald-400">↑</span>
                {displayStats.savings_growth_text}
              </span>
            </div>
            <p className="mt-4 text-sm text-slate-400">
              Month-over-month savings from duplicate request elimination
            </p>
          </article>

          {/* Global Cache Hit Rate */}
          <article className="rounded-3xl border border-slate-800 bg-gradient-to-br from-blue-900/20 to-slate-900/95 p-8 shadow-xl shadow-slate-950/20">
            <p className="text-sm uppercase tracking-[0.2em] text-blue-400">
              Cache Hit Rate
            </p>
            <p className="mt-4 text-4xl font-bold text-blue-300">
              {displayStats.cache_hit_rate.toFixed(1)}%
            </p>
            <p className="mt-4 text-sm text-slate-400">
              of outbound traffic intercepted by local WAL cache
            </p>
            <div className="mt-6 h-1.5 w-full rounded-full bg-slate-800">
              <div
                className="h-full rounded-full bg-gradient-to-r from-blue-500 to-blue-400 transition-all duration-300"
                style={{ width: `${Math.min(displayStats.cache_hit_rate, 100)}%` }}
              ></div>
            </div>
          </article>

          {/* Proxy Health */}
          <article className="rounded-3xl border border-slate-800 bg-gradient-to-br from-cyan-900/20 to-slate-900/95 p-8 shadow-xl shadow-slate-950/20">
            <p className="text-sm uppercase tracking-[0.2em] text-cyan-400">
              Proxy Health
            </p>
            <div className="mt-4 flex items-center gap-2">
              <span
                className={`inline-block h-3 w-3 rounded-full ${statusDot} animate-pulse`}
              ></span>
              <p
                className={`text-2xl font-semibold ${
                  isOnline ? 'text-green-400' : 'text-red-400'
                }`}
              >
                {displayStats.proxy_status}
              </p>
            </div>
            <p className="mt-4 text-sm text-slate-400">
              {displayStats.memory_usage_mb.toFixed(1)}MB RAM ·{' '}
              {displayStats.active_connections} active connections
            </p>
            <p className="mt-4 text-xs font-mono text-slate-500">
              SQLite WAL running in peak health
            </p>
          </article>
        </section>

        {/* Time-Series Chart */}
        <section className="rounded-3xl border border-slate-800 bg-slate-900/90 p-8 shadow-2xl shadow-slate-950/30 backdrop-blur-xl">
          <h2 className="mb-6 text-2xl font-semibold text-white">
            Request Volume Trend (7 Days)
          </h2>
          <div className="h-80 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={mockChartData}>
                <defs>
                  <linearGradient
                    id="colorTotal"
                    x1="0"
                    y1="0"
                    x2="0"
                    y2="1"
                  >
                    <stop offset="5%" stopColor="#64748b" stopOpacity={0.8} />
                    <stop offset="95%" stopColor="#64748b" stopOpacity={0} />
                  </linearGradient>
                  <linearGradient
                    id="colorCached"
                    x1="0"
                    y1="0"
                    x2="0"
                    y2="1"
                  >
                    <stop offset="5%" stopColor="#10b981" stopOpacity={0.8} />
                    <stop offset="95%" stopColor="#10b981" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#334155" />
                <XAxis
                  dataKey="date"
                  stroke="#94a3b8"
                  style={{ fontSize: '12px' }}
                />
                <YAxis stroke="#94a3b8" style={{ fontSize: '12px' }} />
                <Tooltip
                  contentStyle={{
                    backgroundColor: '#1e293b',
                    border: '1px solid #475569',
                    borderRadius: '8px',
                  }}
                  labelStyle={{ color: '#e2e8f0' }}
                />
                <Legend />
                <Area
                  type="monotone"
                  dataKey="totalRequests"
                  stroke="#94a3b8"
                  fillOpacity={1}
                  fill="url(#colorTotal)"
                  name="Total Vendor API Requests"
                  isAnimationActive={false}
                />
                <Area
                  type="monotone"
                  dataKey="cachedRequests"
                  stroke="#10b981"
                  fillOpacity={1}
                  fill="url(#colorCached)"
                  name="Requests Served via Cache"
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </section>

        {/* Live Interception Feed */}
        <section className="rounded-3xl border border-slate-800 bg-slate-900/90 p-8 shadow-2xl shadow-slate-950/30 backdrop-blur-xl">
          <h2 className="mb-6 text-2xl font-semibold text-white">
            Live Interception Feed
          </h2>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-700">
                  <th className="px-4 py-3 text-left font-semibold text-slate-300">
                    Timestamp
                  </th>
                  <th className="px-4 py-3 text-left font-semibold text-slate-300">
                    Target Endpoint
                  </th>
                  <th className="px-4 py-3 text-center font-semibold text-slate-300">
                    Action
                  </th>
                  <th className="px-4 py-3 text-right font-semibold text-slate-300">
                    Latency
                  </th>
                  <th className="px-4 py-3 text-right font-semibold text-slate-300">
                    TTL Remaining
                  </th>
                </tr>
              </thead>
              <tbody>
                {displayStats.recent_interceptions.map((event, idx) => (
                  <tr
                    key={idx}
                    className="border-b border-slate-700/50 hover:bg-slate-800/30 transition-colors"
                  >
                    <td className="px-4 py-3 font-mono text-slate-300">
                      {typeof event.timestamp === 'string'
                        ? event.timestamp
                        : new Date(event.timestamp).toLocaleTimeString()}
                    </td>
                    <td className="px-4 py-3 text-slate-300">
                      {event.target_endpoint}
                    </td>
                    <td className="px-4 py-3 text-center">
                      {event.action === 'CACHE_HIT' ? (
                        <span className="inline-flex items-center gap-2 rounded-full bg-emerald-900/30 px-3 py-1 text-xs font-semibold text-emerald-400">
                          <span className="h-2 w-2 rounded-full bg-emerald-400"></span>
                          CACHE HIT
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-2 rounded-full bg-slate-700/50 px-3 py-1 text-xs font-semibold text-slate-400">
                          <span className="h-2 w-2 rounded-full bg-slate-500"></span>
                          VENDOR CALL
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-slate-300">
                      {event.latency_ms.toFixed(2)}ms
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-slate-400">
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

