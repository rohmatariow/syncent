import React, { useState, useEffect } from 'react';
import * as api from '../api/client';

function MetricBar({ label, value, unit = '%', warn = 70, crit = 90 }) {
  const color = value >= crit ? 'bg-red-400' : value >= warn ? 'bg-amber-400' : 'bg-emerald-400';
  return (
    <div className="space-y-1.5">
      <div className="flex justify-between text-xs">
        <span className="text-zinc-500 uppercase tracking-wider font-medium">{label}</span>
        <span className="text-zinc-300 font-mono">{value != null ? `${value.toFixed(1)}${unit}` : '—'}</span>
      </div>
      <div className="h-2 bg-zinc-800 rounded-full overflow-hidden">
        <div className={`h-full rounded-full transition-all duration-700 ${color}`} style={{ width: `${Math.min(value || 0, 100)}%` }} />
      </div>
    </div>
  );
}

function InfoRow({ label, value, color }) {
  return (
    <div className="flex justify-between">
      <span className="text-zinc-500">{label}</span>
      <span className={`font-mono text-xs ${color || 'text-zinc-300'}`}>{value || '—'}</span>
    </div>
  );
}

export default function MetricsPanel({ server, onRefresh }) {
  const [metrics, setMetrics] = useState(null);
  const [availability, setAvailability] = useState(null);
  const [serverDetail, setServerDetail] = useState(null);
  const [loading, setLoading] = useState(false);
  const [collecting, setCollecting] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const [m, a, d] = await Promise.all([
        api.getCurrentMetrics(server.id),
        api.getAvailability(server.id),
        api.getServer(server.id).catch(() => null),
      ]);
      setMetrics(m); setAvailability(a);
      if (d) setServerDetail(d);
    } catch {}
    setLoading(false);
  };

  useEffect(() => { load(); }, [server.id]);

  const handleCollect = async () => {
    setCollecting(true);
    try { const res = await api.collectMetrics(server.id); setMetrics(res); onRefresh(); } catch {}
    setCollecting(false);
  };

  const m = metrics?.metrics;
  const d = serverDetail;
  const isGsocket = server.connection_mode === 'gsocket';
  const statusColor = server.status === 'online' ? 'text-emerald-400' : server.status === 'pending_setup' ? 'text-zinc-500' : 'text-red-400';

  return (
    <div className="space-y-6">
      <div className="flex gap-2">
        <button onClick={handleCollect} disabled={collecting}
          className="px-3 py-1.5 bg-zinc-800 hover:bg-zinc-700 text-zinc-300 text-xs rounded-lg disabled:opacity-50">
          {collecting ? 'Collecting...' : 'Collect Now'}
        </button>
        <button onClick={load} className="px-3 py-1.5 bg-zinc-800 hover:bg-zinc-700 text-zinc-300 text-xs rounded-lg">Refresh</button>
      </div>

      {/* Metric Bars */}
      <div className="grid grid-cols-3 gap-4">
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5">
          <MetricBar label="CPU" value={m?.cpu_percent} />
        </div>
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5">
          <MetricBar label="RAM" value={m?.ram_percent} />
          {m?.ram_used_mb != null && <p className="text-xs text-zinc-600 mt-1">{m.ram_used_mb}MB / {m.ram_total_mb}MB</p>}
        </div>
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5">
          <MetricBar label="Disk" value={m?.disk_percent} warn={75} crit={90} />
          {m?.disk_used_gb != null && <p className="text-xs text-zinc-600 mt-1">{m.disk_used_gb?.toFixed(1)}GB / {m.disk_total_gb?.toFixed(1)}GB</p>}
        </div>
      </div>

      {/* Info Cards */}
      <div className="grid grid-cols-2 gap-4">
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 space-y-3">
          <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Server Details</h3>
          <div className="space-y-2 text-sm">
            <InfoRow label="Status" value={server.status?.toUpperCase()} color={statusColor} />
            <InfoRow label="Connection" value={isGsocket ? 'GSSocket' : 'SSH'} />
            <InfoRow label="Latency" value={server.last_ping_ms != null ? `${server.last_ping_ms.toFixed(0)}ms` : null} />
            <InfoRow label="OS" value={d?.os_info || server.os_info || null} />
            <InfoRow label="Kernel" value={d?.kernel_info || null} />
            <InfoRow label="Location" value={d?.location || server.location || null} />
            <InfoRow label="User" value={(() => {
              const u = d?.ssh_username || null;
              if (!u || u === 'gsocket') return null;
              return u;
            })()} />
            {!isGsocket && <InfoRow label="SSH Port" value={String(server.ssh_port || 22)} />}
            <InfoRow label="GPU" value="—" />
            <InfoRow label="VRAM" value="—" />
            <InfoRow label="Tags" value={(() => {
              const t = server.tags || d?.tags;
              if (Array.isArray(t) && t.length > 0) return t.join(', ');
              return null;
            })()} />
          </div>
        </div>

        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 space-y-3">
          <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Availability</h3>
          {availability?.availability ? (
            <div className="space-y-3">
              {['24h', '7d', '30d'].map(period => {
                const pct = availability.availability[period];
                if (pct == null) return null;
                const color = pct >= 99 ? 'text-emerald-400' : pct >= 95 ? 'text-amber-400' : 'text-red-400';
                const barColor = pct >= 99 ? 'bg-emerald-400' : pct >= 95 ? 'bg-amber-400' : 'bg-red-400';
                return (
                  <div key={period} className="space-y-1">
                    <div className="flex justify-between text-xs">
                      <span className="text-zinc-500">{period}</span>
                      <span className={`font-mono ${color}`}>{pct.toFixed(2)}%</span>
                    </div>
                    <div className="h-1.5 bg-zinc-800 rounded-full overflow-hidden">
                      <div className={`h-full rounded-full ${barColor}`} style={{ width: `${pct}%` }} />
                    </div>
                  </div>
                );
              })}
            </div>
          ) : <p className="text-xs text-zinc-600">No availability data yet.</p>}
        </div>
      </div>

      {/* Network */}
      {m && (m.net_in_bytes != null || m.net_out_bytes != null) && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5">
          <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium mb-3">Network (cumulative)</h3>
          <div className="grid grid-cols-2 gap-4 text-sm">
            <InfoRow label="In" value={formatBytes(m.net_in_bytes)} />
            <InfoRow label="Out" value={formatBytes(m.net_out_bytes)} />
          </div>
        </div>
      )}

      {/* Top Processes */}
      {m?.top_processes && m.top_processes.length > 0 && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
          <div className="px-5 py-3 border-b border-zinc-800">
            <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Top Processes</h3>
          </div>
          <div className="divide-y divide-zinc-800/50">
            {m.top_processes.map((p, i) => {
              const proc = typeof p === 'string' ? JSON.parse(p) : p;
              return (
                <div key={i} className="flex items-center gap-4 px-5 py-2.5 text-xs">
                  <span className="text-zinc-600 w-4">{i + 1}</span>
                  <span className="text-zinc-300 flex-1 font-mono truncate">{proc.name}</span>
                  <span className="text-zinc-500 w-16 text-right">CPU {proc.cpu}%</span>
                  <span className="text-zinc-500 w-16 text-right">RAM {proc.ram}%</span>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {!m && !loading && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-12 text-center">
          <p className="text-zinc-500 text-sm">No metrics yet. Click "Collect Now" or wait for automatic collection (every 5 min).</p>
        </div>
      )}

      {m?.recorded_at && <p className="text-xs text-zinc-600">Last collected: {new Date(m.recorded_at).toLocaleString()}</p>}
    </div>
  );
}

function formatBytes(bytes) {
  if (bytes == null) return '—';
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
  if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB';
  return (bytes / 1073741824).toFixed(2) + ' GB';
}
