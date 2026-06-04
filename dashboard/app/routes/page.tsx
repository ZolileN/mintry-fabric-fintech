'use client';

import { useEffect, useState } from 'react';

export default function RoutesPage() {
  const [yamlConfig, setYamlConfig] = useState<string>('');
  const [loading, setLoading] = useState<boolean>(true);
  const [saving, setSaving] = useState<boolean>(false);
  const [statusMessage, setStatusMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  useEffect(() => {
    const fetchRoutes = async () => {
      try {
        const res = await fetch('http://localhost:8081/api/routes');
        if (!res.ok) {
          throw new Error(`Failed to load config: ${res.statusText}`);
        }
        const data = await res.json();
        setYamlConfig(data.config_yaml);
      } catch (err) {
        console.warn('API unavailable, displaying default routes fallback.');
        setYamlConfig(`# Fallback configuration\nca_cert: "../mintry-root.crt"\nca_key: "../mintry-root.key"\nroutes:\n  - match: "api.transunion.co.za/v1/score"\n    ttl: "30d"`);
      } finally {
        setLoading(false);
      }
    };
    fetchRoutes();
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setStatusMessage(null);
    try {
      const res = await fetch('http://localhost:8081/api/routes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ config_yaml: yamlConfig }),
      });

      if (!res.ok) {
        const errMsg = await res.text();
        throw new Error(errMsg || `Status code ${res.status}`);
      }

      setStatusMessage({ type: 'success', text: 'Configuration saved and hot-reloaded successfully!' });
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Unknown network error';
      setStatusMessage({ type: 'error', text: `Failed to save: ${msg}` });
    } finally {
      setSaving(false);
    }
  };

  return (
    <main className="min-h-screen w-full flex-1 p-8 transition-colors duration-200 relative z-10">
      <div className="mx-auto max-w-7xl space-y-8">
        {/* Header */}
        <section className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-8 shadow-xl shadow-slate-200/30 dark:shadow-none">
          <p className="text-xs uppercase tracking-[0.3em] font-semibold text-emerald-600 dark:text-emerald-400">
            Policy Engine
          </p>
          <h1 className="mt-4 text-3xl font-bold tracking-tight text-slate-900 dark:text-white">
            Cache Routing Rules & TTL Policies
          </h1>
          <p className="mt-4 max-w-3xl text-sm text-slate-500 dark:text-slate-400 leading-relaxed">
            Mintry Fabric selectively intercepts and caches traffic matching the match statements below. Updates are validated for YAML syntax correctness and hot-reloaded in real-time across all active proxy routing routines.
          </p>
        </section>

        {/* Editor & Actions Container */}
        <section className="grid gap-8 lg:grid-cols-3">
          {/* Main YAML Editor Card */}
          <div className="lg:col-span-2 rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-6 shadow-xl shadow-slate-200/20 dark:shadow-none flex flex-col space-y-6">
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-bold text-slate-900 dark:text-white">
                config.yaml
              </h2>
              <span className="text-[10px] font-mono uppercase tracking-wider bg-slate-100 dark:bg-slate-800 px-2 py-1 rounded text-slate-500 dark:text-slate-400">
                YAML Rules File
              </span>
            </div>

            {loading ? (
              <div className="h-96 w-full flex items-center justify-center bg-slate-100 dark:bg-slate-950 rounded-2xl animate-pulse">
                <span className="text-sm font-semibold text-slate-400">Loading Configuration...</span>
              </div>
            ) : (
              <textarea
                value={yamlConfig}
                onChange={(e) => setYamlConfig(e.target.value)}
                className="w-full h-[500px] font-mono text-xs p-5 bg-slate-950 text-slate-100 border border-slate-800 rounded-2xl focus:outline-none focus:ring-2 focus:ring-emerald-500/50 resize-none shadow-inner"
                spellCheck="false"
              />
            )}

            <div className="flex items-center justify-end gap-4">
              <button
                onClick={handleSave}
                disabled={loading || saving}
                className="px-6 py-3 rounded-xl bg-gradient-to-r from-emerald-600 to-teal-500 hover:from-emerald-500 hover:to-teal-400 text-white font-bold text-sm shadow-lg shadow-emerald-500/20 hover:shadow-emerald-500/30 transition-all flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {saving ? (
                  <>
                    <span className="h-4 w-4 border-2 border-white/30 border-t-white rounded-full animate-spin"></span>
                    Validating & Saving...
                  </>
                ) : (
                  <>
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 7H5a2 2 0 00-2 2v9a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-3m-1 4l-3 3m0 0l-3-3m3 3V4" />
                    </svg>
                    Apply Policies
                  </>
                )}
              </button>
            </div>

            {statusMessage && (
              <div
                className={`p-4 rounded-xl text-xs font-semibold flex gap-2 border ${
                  statusMessage.type === 'success'
                    ? 'bg-emerald-50 dark:bg-emerald-950/20 border-emerald-200/50 dark:border-emerald-800/30 text-emerald-700 dark:text-emerald-350'
                    : 'bg-red-50 dark:bg-red-950/20 border-red-200/50 dark:border-red-800/30 text-red-700 dark:text-red-350'
                }`}
              >
                <span>{statusMessage.type === 'success' ? '✓' : '⚠️'}</span>
                <p className="whitespace-pre-wrap">{statusMessage.text}</p>
              </div>
            )}
          </div>

          {/* Reference Policy Guide */}
          <div className="rounded-3xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900/90 p-6 shadow-xl shadow-slate-200/20 dark:shadow-none space-y-6">
            <h2 className="text-lg font-bold text-slate-900 dark:text-white">
              Policy Quick Reference
            </h2>
            <div className="space-y-4 text-xs">
              <div className="border-l-2 border-slate-350 dark:border-slate-700 pl-3">
                <p className="font-bold text-slate-700 dark:text-slate-300">Route Pattern matching</p>
                <p className="text-slate-500 dark:text-slate-400 mt-1 leading-relaxed">
                  Route match filters are case-insensitive and match host+path strings. (e.g. <code className="font-mono text-emerald-600 dark:text-emerald-400 bg-slate-100 dark:bg-slate-950 px-1 rounded">api.transunion.co.za/v1/score</code>).
                </p>
              </div>

              <div className="border-l-2 border-slate-350 dark:border-slate-700 pl-3">
                <p className="font-bold text-slate-700 dark:text-slate-300">TTL Durations</p>
                <p className="text-slate-500 dark:text-slate-400 mt-1 leading-relaxed">
                  Specify TTLs with standard tags like <code className="font-mono bg-slate-100 dark:bg-slate-950 px-1 rounded">d</code> (days), <code className="font-mono bg-slate-100 dark:bg-slate-950 px-1 rounded">h</code> (hours), <code className="font-mono bg-slate-100 dark:bg-slate-950 px-1 rounded">m</code> (minutes), or <code className="font-mono bg-slate-100 dark:bg-slate-950 px-1 rounded">s</code> (seconds). (e.g. <code className="font-mono text-emerald-600 dark:text-emerald-400 bg-slate-100 dark:bg-slate-950 px-1 rounded">30d</code>).
                </p>
              </div>

              <div className="border-l-2 border-slate-350 dark:border-slate-700 pl-3">
                <p className="font-bold text-slate-700 dark:text-slate-300">Cache Errors Flag</p>
                <p className="text-slate-500 dark:text-slate-400 mt-1 leading-relaxed">
                  Set <code className="font-mono bg-slate-100 dark:bg-slate-950 px-1 rounded">cache_errors: true</code> to cache non-200 API responses (highly recommended for protecting downstream systems during vendor outages).
                </p>
              </div>
            </div>

            <div className="p-4 rounded-2xl bg-amber-50 dark:bg-amber-950/20 border border-amber-250/20 text-xs text-amber-700 dark:text-amber-350 leading-relaxed space-y-2">
              <p className="font-bold">⚠️ Warning: Root CA Keys</p>
              <p>Do not modify the <code className="font-mono bg-slate-150 dark:bg-slate-950 px-1 rounded">ca_cert</code> and <code className="font-mono bg-slate-150 dark:bg-slate-950 px-1 rounded">ca_key</code> paths unless you have re-imported those certificates in the microservices.</p>
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}
