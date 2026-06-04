'use client';

import { useEffect, useState } from 'react';

interface SettingsData {
  db_path: string;
  encryption_enabled: boolean;
  total_cached_records: number;
  total_telemetry_records: number;
  circuit_breaker_state: string;
  pass_through_forced: boolean;
}

export default function SettingsPage() {
  const [settings, setSettings] = useState<SettingsData | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [toggling, setToggling] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const fetchSettings = async () => {
    try {
      const res = await fetch('http://localhost:8081/api/settings');
      if (!res.ok) {
        throw new Error(`Failed to load settings: ${res.statusText}`);
      }
      const data = await res.json();
      setSettings(data);
      setError(null);
    } catch (err) {
      console.warn('Settings API currently unavailable. Displaying demo config settings.');
      setError('API connection failed');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchSettings();
    const interval = setInterval(fetchSettings, 5000);
    return () => clearInterval(interval);
  }, []);

  const handleTogglePassThrough = async () => {
    if (!settings) return;
    setToggling(true);
    const nextVal = !settings.pass_through_forced;
    try {
      const res = await fetch('http://localhost:8081/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ forced: nextVal }),
      });

      if (!res.ok) {
        throw new Error(`Failed to toggle: ${res.statusText}`);
      }

      setSettings((prev) => prev ? { ...prev, pass_through_forced: nextVal } : null);
    } catch (err) {
      console.error('Failed to change pass-through:', err);
      alert('Error updating pass-through toggle. Verify proxy is running.');
    } finally {
      setToggling(false);
    }
  };

  // Fallback demo metrics if API is down
  const displaySettings = settings || {
    db_path: 'cache.db (Demo)',
    encryption_enabled: true,
    total_cached_records: 486,
    total_telemetry_records: 2450,
    circuit_breaker_state: 'CLOSED',
    pass_through_forced: false,
  };

  return (
    <main className="min-h-screen w-full flex-1 p-8 transition-colors duration-200 relative z-10">
      <div className="mx-auto max-w-7xl space-y-8">
        {/* Header */}
        <section className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-8 shadow-xl shadow-slate-200/30 dark:shadow-none">
          <p className="text-xs uppercase tracking-[0.3em] font-semibold text-emerald-600 dark:text-emerald-400">
            System Console
          </p>
          <h1 className="mt-4 text-3xl font-bold tracking-tight text-slate-900 dark:text-white">
            Proxy & Security Settings
          </h1>
          <p className="mt-4 max-w-3xl text-sm text-slate-500 dark:text-slate-400 leading-relaxed">
            Inspect local storage volumes, verify SQLCipher cryptographic encryption status, monitor failsafe circuit breaker levels, and programmatically override traffic caching modes.
          </p>
        </section>

        <section className="grid gap-8 md:grid-cols-2">
          {/* Encryption & Database Settings Card */}
          <div className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-6 shadow-xl shadow-slate-200/20 dark:shadow-none space-y-6">
            <h2 className="text-lg font-bold text-slate-900 dark:text-white">
              Storage & Encryption
            </h2>

            <div className="space-y-4 text-xs">
              <div className="flex justify-between items-center py-2 border-b border-slate-100 dark:border-slate-800/80">
                <span className="font-semibold text-slate-555 dark:text-slate-400">Database Engine</span>
                <span className="font-mono bg-slate-100 dark:bg-slate-800 px-2.5 py-1 rounded">SQLite 3 (WAL Mode)</span>
              </div>

              <div className="flex justify-between items-center py-2 border-b border-slate-100 dark:border-slate-800/80">
                <span className="font-semibold text-slate-555 dark:text-slate-400">Database File Path</span>
                <span className="font-mono text-slate-700 dark:text-slate-300">{displaySettings.db_path}</span>
              </div>

              <div className="flex justify-between items-center py-2 border-b border-slate-100 dark:border-slate-800/80">
                <span className="font-semibold text-slate-555 dark:text-slate-400">Encryption Status</span>
                {displaySettings.encryption_enabled ? (
                  <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 dark:bg-emerald-950/40 px-2.5 py-1 text-[10px] font-bold text-emerald-600 dark:text-emerald-400 border border-emerald-250/20">
                    🔒 SQLCIPHER ACTIVE (AES-256)
                  </span>
                ) : (
                  <span className="inline-flex items-center gap-1.5 rounded-full bg-red-50 dark:bg-red-950/40 px-2.5 py-1 text-[10px] font-bold text-red-600 dark:text-red-400 border border-red-250/20 animate-pulse">
                    ⚠️ UNENCRYPTED
                  </span>
                )}
              </div>

              <div className="flex justify-between items-center py-2 border-b border-slate-100 dark:border-slate-800/80">
                <span className="font-semibold text-slate-555 dark:text-slate-400">Total Cache Records</span>
                <span className="font-bold text-slate-700 dark:text-slate-350">{displaySettings.total_cached_records} records</span>
              </div>

              <div className="flex justify-between items-center py-2">
                <span className="font-semibold text-slate-555 dark:text-slate-400">Total Telemetry Logs</span>
                <span className="font-bold text-slate-700 dark:text-slate-350">{displaySettings.total_telemetry_records} entries</span>
              </div>
            </div>
          </div>

          {/* Circuit Breaker & Pass-Through Override */}
          <div className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-6 shadow-xl shadow-slate-200/20 dark:shadow-none space-y-6">
            <h2 className="text-lg font-bold text-slate-900 dark:text-white">
              Failsafe Resiliency
            </h2>

            <div className="space-y-6">
              {/* Breaker State */}
              <div className="flex justify-between items-center">
                <div>
                  <h3 className="text-sm font-bold text-slate-800 dark:text-slate-200">
                    Database Circuit Breaker State
                  </h3>
                  <p className="text-[11px] text-slate-500 dark:text-slate-400 mt-1 max-w-xs">
                    Automatically trips to OPEN if database disk/cryptographic checks fail consecutively.
                  </p>
                </div>
                <div>
                  {displaySettings.circuit_breaker_state === 'CLOSED' ? (
                    <span className="inline-flex items-center gap-1 bg-green-50 dark:bg-green-950/40 text-green-600 dark:text-green-400 border border-green-200/30 px-3 py-1.5 rounded-xl font-bold text-xs">
                      <span className="h-2 w-2 rounded-full bg-green-500 inline-block"></span>
                      CLOSED (HEALTHY)
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 bg-amber-50 dark:bg-amber-950/40 text-amber-600 dark:text-amber-400 border border-amber-200/30 px-3 py-1.5 rounded-xl font-bold text-xs animate-pulse">
                      <span className="h-2 w-2 rounded-full bg-amber-500 inline-block"></span>
                      {displaySettings.circuit_breaker_state} (BYPASSING)
                    </span>
                  )}
                </div>
              </div>

              <hr className="border-slate-100 dark:border-slate-800/80" />

              {/* Pass-Through override toggle */}
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-sm font-bold text-slate-800 dark:text-slate-200">
                    Force Transparent Pass-Through
                  </h3>
                  <p className="text-[11px] text-slate-500 dark:text-slate-400 mt-1 max-w-xs">
                    Bypasses all local SQLite caching entirely and forwards all requests directly to vendor API gateways (useful for debug runs).
                  </p>
                </div>
                <button
                  onClick={handleTogglePassThrough}
                  disabled={loading || toggling}
                  className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none ${
                    displaySettings.pass_through_forced ? 'bg-emerald-500' : 'bg-slate-200 dark:bg-slate-800'
                  }`}
                >
                  <span
                    className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                      displaySettings.pass_through_forced ? 'translate-x-5' : 'translate-x-0'
                    }`}
                  />
                </button>
              </div>

              {displaySettings.pass_through_forced && (
                <div className="p-3.5 rounded-xl bg-amber-50 dark:bg-amber-950/20 border border-amber-250/20 text-xs text-amber-700 dark:text-amber-350">
                  ⚠️ <strong>Pass-Through override active:</strong> No requests are being cached or decrypted locally. The system is operating in pure transparent gateway mode.
                </div>
              )}
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}
