export interface MetricsSnapshot {
  totalSavedZAR: number;
  monthlyGrowthPercent: number;
  cacheHitRate: number;
  proxyStatus: 'Online' | 'Offline' | 'Degraded';
  ramUsageMB: number;
  activeConnections: number;
}

export interface ChartDataPoint {
  date: string;
  totalRequests: number;
  cachedRequests: number;
}

export interface InterceptionEvent {
  timestamp: string;
  endpoint: string;
  action: 'CACHE_HIT' | 'VENDOR_CALL';
  latencyMs: number;
  ttlRemaining: string;
}

export const mockMetrics: MetricsSnapshot = {
  totalSavedZAR: 42500,
  monthlyGrowthPercent: 12,
  cacheHitRate: 34.2,
  proxyStatus: 'Online',
  ramUsageMB: 14,
  activeConnections: 7,
};

export const mockChartData: ChartDataPoint[] = [
  { date: '2026-05-29', totalRequests: 1250, cachedRequests: 380 },
  { date: '2026-05-30', totalRequests: 1340, cachedRequests: 421 },
  { date: '2026-05-31', totalRequests: 1180, cachedRequests: 355 },
  { date: '2026-06-01', totalRequests: 1560, cachedRequests: 520 },
  { date: '2026-06-02', totalRequests: 1420, cachedRequests: 468 },
  { date: '2026-06-03', totalRequests: 1650, cachedRequests: 582 },
  { date: '2026-06-04', totalRequests: 1480, cachedRequests: 495 },
];

export const mockInterceptionEvents: InterceptionEvent[] = [
  {
    timestamp: '14:02:33',
    endpoint: 'api.transunion.co.za/v1/score',
    action: 'CACHE_HIT',
    latencyMs: 1.2,
    ttlRemaining: '12d 4h',
  },
  {
    timestamp: '14:01:58',
    endpoint: 'api.smileid.com/v1/async/verify',
    action: 'VENDOR_CALL',
    latencyMs: 342,
    ttlRemaining: '7d 2h',
  },
  {
    timestamp: '14:01:22',
    endpoint: 'api.transunion.co.za/v1/score',
    action: 'CACHE_HIT',
    latencyMs: 0.8,
    ttlRemaining: '12d 3h',
  },
  {
    timestamp: '14:00:55',
    endpoint: 'api.smileid.com/v1/async/verify',
    action: 'CACHE_HIT',
    latencyMs: 1.5,
    ttlRemaining: '6d 23h',
  },
  {
    timestamp: '14:00:18',
    endpoint: 'api.transunion.co.za/v1/score',
    action: 'VENDOR_CALL',
    latencyMs: 385,
    ttlRemaining: 'New',
  },
  {
    timestamp: '13:59:42',
    endpoint: 'api.smileid.com/v1/async/verify',
    action: 'CACHE_HIT',
    latencyMs: 1.1,
    ttlRemaining: '6d 22h',
  },
  {
    timestamp: '13:59:05',
    endpoint: 'api.transunion.co.za/v1/score',
    action: 'CACHE_HIT',
    latencyMs: 0.9,
    ttlRemaining: '12d 2h',
  },
  {
    timestamp: '13:58:33',
    endpoint: 'api.smileid.com/v1/async/verify',
    action: 'VENDOR_CALL',
    latencyMs: 298,
    ttlRemaining: 'New',
  },
];
