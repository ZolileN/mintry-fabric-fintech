'use client';

import { useEffect, useState } from 'react';
import { mockInterceptionEvents } from '@/lib/mockData';

interface InterceptionEvent {
  timestamp: string;
  target_endpoint: string;
  action: string;
  latency_ms: number;
  ttl_remaining: string;
}

export default function FeedPage() {
  const [logs, setLogs] = useState<InterceptionEvent[]>([]);
  const [search, setSearch] = useState<string>('');
  const [filterAction, setFilterAction] = useState<string>('ALL');
  const [currentPage, setCurrentPage] = useState<number>(1);
  const [wsConnected, setWsConnected] = useState<boolean>(false);
  const itemsPerPage = 12;

  useEffect(() => {
    // 1. Fetch initial logs
    const fetchInitialLogs = async () => {
      try {
        const res = await fetch('http://localhost:8081/metrics');
        if (res.ok) {
          const data = await res.json();
          if (data.recent_interceptions) {
            setLogs(
              data.recent_interceptions.map((e: any) => ({
                timestamp: e.timestamp,
                target_endpoint: e.target_endpoint,
                action: e.action,
                latency_ms: e.latency_ms,
                ttl_remaining: e.ttl_remaining,
              }))
            );
          }
        }
      } catch (err) {
        console.warn('API metrics unreachable. Falling back to mock feed.');
        setLogs(
          mockInterceptionEvents.map((e) => ({
            timestamp: e.timestamp,
            target_endpoint: e.endpoint,
            action: e.action,
            latency_ms: e.latencyMs,
            ttl_remaining: e.ttlRemaining,
          }))
        );
      }
    };
    fetchInitialLogs();

    // 2. Open WebSocket stream
    let ws: WebSocket;
    const connectWS = () => {
      ws = new WebSocket('ws://localhost:8081/ws/feed');

      ws.onopen = () => {
        setWsConnected(true);
        console.log('Telemetry WS connected');
      };

      ws.onmessage = (event) => {
        try {
          const newEntry: InterceptionEvent = JSON.parse(event.data);
          setLogs((prev) => {
            // Check if log is already present (avoid duplicates if polling overlaps)
            const exists = prev.some(
              (x) => x.timestamp === newEntry.timestamp && x.target_endpoint === newEntry.target_endpoint
            );
            if (exists) return prev;
            return [newEntry, ...prev].slice(0, 500); // Cap at 500 records
          });
        } catch (err) {
          console.error('Failed to parse WS data frame:', err);
        }
      };

      ws.onclose = () => {
        setWsConnected(false);
        console.log('Telemetry WS closed. Reconnecting in 3s...');
        setTimeout(connectWS, 3000);
      };

      ws.onerror = () => {
        ws.close();
      };
    };

    connectWS();

    return () => {
      if (ws) {
        ws.close();
      }
    };
  }, []);

  // Filter and search
  const filteredLogs = logs.filter((log) => {
    const matchesSearch = log.target_endpoint.toLowerCase().includes(search.toLowerCase());
    const matchesFilter = filterAction === 'ALL' || log.action === filterAction;
    return matchesSearch && matchesFilter;
  });

  // Pagination logic
  const totalPages = Math.ceil(filteredLogs.length / itemsPerPage) || 1;
  const startIndex = (currentPage - 1) * itemsPerPage;
  const paginatedLogs = filteredLogs.slice(startIndex, startIndex + itemsPerPage);

  const handlePageChange = (page: number) => {
    if (page >= 1 && page <= totalPages) {
      setCurrentPage(page);
    }
  };

  return (
    <main className="min-h-screen w-full flex-1 p-8 transition-colors duration-200 relative z-10">
      <div className="mx-auto max-w-7xl space-y-8">
        {/* Header */}
        <section className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-8 shadow-xl shadow-slate-200/30 dark:shadow-none flex flex-col md:flex-row md:items-center md:justify-between gap-6">
          <div className="space-y-2">
            <p className="text-xs uppercase tracking-[0.3em] font-semibold text-emerald-600 dark:text-emerald-400">
              Live Stream
            </p>
            <h1 className="text-3xl font-bold tracking-tight text-slate-900 dark:text-white">
              Real-Time Interception Feed
            </h1>
            <p className="max-w-xl text-sm text-slate-500 dark:text-slate-400">
              Outbound vendor endpoints matched by Mintry Fabric are decrypted, logged, and streamed below.
            </p>
          </div>
          <div className="flex items-center gap-3">
            <span className={`h-2.5 w-2.5 rounded-full ${wsConnected ? 'bg-green-500' : 'bg-amber-500 animate-pulse'}`}></span>
            <span className="text-xs font-semibold text-slate-600 dark:text-slate-400 font-mono">
              {wsConnected ? 'STREAM ACTIVE (WEBSOCKET)' : 'OFFLINE (POLLING FALLBACK)'}
            </span>
          </div>
        </section>

        {/* Filter Controls Row */}
        <section className="flex flex-col sm:flex-row gap-4 items-center justify-between">
          <div className="relative w-full sm:w-80">
            <input
              type="text"
              placeholder="Search target endpoint..."
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setCurrentPage(1);
              }}
              className="w-full pl-10 pr-4 py-3 rounded-2xl bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800 text-xs focus:outline-none focus:ring-2 focus:ring-emerald-500/40 text-slate-700 dark:text-slate-200 shadow-sm"
            />
            <span className="absolute left-4 top-3.5 text-slate-400">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
            </span>
          </div>

          <div className="flex items-center gap-2 w-full sm:w-auto overflow-x-auto pb-1 sm:pb-0">
            {['ALL', 'CACHE_HIT', 'VENDOR_CALL'].map((act) => (
              <button
                key={act}
                onClick={() => {
                  setFilterAction(act);
                  setCurrentPage(1);
                }}
                className={`px-4 py-2.5 rounded-xl text-xs font-bold transition-all ${
                  filterAction === act
                    ? 'bg-slate-900 text-white dark:bg-white dark:text-slate-950 shadow-sm'
                    : 'bg-white text-slate-600 border border-slate-200 hover:bg-slate-50 dark:bg-slate-900 dark:text-slate-400 dark:border-slate-800 dark:hover:bg-slate-800/80'
                }`}
              >
                {act === 'ALL' ? 'All Actions' : act === 'CACHE_HIT' ? 'Cache Hits' : 'Vendor Calls'}
              </button>
            ))}
          </div>
        </section>

        {/* Logs Table Card */}
        <section className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 shadow-xl shadow-slate-200/30 dark:shadow-none p-6">
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
                {paginatedLogs.length > 0 ? (
                  paginatedLogs.map((log, idx) => (
                    <tr
                      key={idx}
                      className="hover:bg-slate-100/50 dark:hover:bg-slate-800/40 transition-colors animate-fadeIn"
                    >
                      <td className="px-4 py-3.5 font-mono text-slate-500 dark:text-slate-400">
                        {typeof log.timestamp === 'string' && log.timestamp.includes('T')
                          ? log.timestamp.split('T')[1].substring(0, 8)
                          : typeof log.timestamp === 'string'
                          ? log.timestamp
                          : new Date(log.timestamp).toLocaleTimeString()}
                      </td>
                      <td className="px-4 py-3.5 font-medium text-slate-700 dark:text-slate-300">
                        {log.target_endpoint}
                      </td>
                      <td className="px-4 py-3.5 text-center">
                        {log.action === 'CACHE_HIT' ? (
                          <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 dark:bg-emerald-950/40 px-2.5 py-0.5 text-[10px] font-bold text-emerald-600 dark:text-emerald-400 border border-emerald-200/30 dark:border-emerald-800/20">
                            <span className="h-1.5 w-1.5 rounded-full bg-emerald-500"></span>
                            CACHE HIT
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1.5 rounded-full bg-slate-100 dark:bg-slate-800/50 px-2.5 py-0.5 text-[10px] font-bold text-slate-600 dark:text-slate-450 border border-slate-200 dark:border-slate-800/30">
                            <span className="h-1.5 w-1.5 rounded-full bg-slate-400 dark:bg-slate-555"></span>
                            VENDOR CALL
                          </span>
                        )}
                      </td>
                      <td className="px-4 py-3.5 text-right font-mono font-medium text-slate-600 dark:text-slate-300">
                        {log.latency_ms.toFixed(2)}ms
                      </td>
                      <td className="px-4 py-3.5 text-right font-mono text-slate-500 dark:text-slate-450">
                        {log.ttl_remaining}
                      </td>
                    </tr>
                  ))
                ) : (
                  <tr>
                    <td colSpan={5} className="px-4 py-10 text-center text-slate-455 font-medium">
                      No interception records matched your search filters.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          {/* Pagination Controls */}
          <div className="flex items-center justify-between border-t border-slate-200 dark:border-slate-800 mt-6 pt-6">
            <span className="text-xs text-slate-555 dark:text-slate-400 font-medium">
              Showing {filteredLogs.length > 0 ? startIndex + 1 : 0} to{' '}
              {Math.min(startIndex + itemsPerPage, filteredLogs.length)} of {filteredLogs.length} entries
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={() => handlePageChange(currentPage - 1)}
                disabled={currentPage === 1}
                className="p-2 rounded-lg border border-slate-200 dark:border-slate-800 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors disabled:opacity-40 disabled:hover:bg-transparent"
              >
                <svg className="w-4 h-4 text-slate-500 dark:text-slate-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
                </svg>
              </button>
              <span className="text-xs font-bold font-mono px-3">
                Page {currentPage} of {totalPages}
              </span>
              <button
                onClick={() => handlePageChange(currentPage + 1)}
                disabled={currentPage === totalPages}
                className="p-2 rounded-lg border border-slate-200 dark:border-slate-800 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors disabled:opacity-40 disabled:hover:bg-transparent"
              >
                <svg className="w-4 h-4 text-slate-500 dark:text-slate-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                </svg>
              </button>
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}
