import React, { useState, useEffect } from 'react';
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer, PieChart, Pie, Cell } from 'recharts';
import * as api from '../api/client';

const COLORS = ['#34d399', '#f87171', '#fbbf24', '#71717a'];

export default function HomeDashboard({ servers, onSelectServer }) {
  const [dashboard, setDashboard] = useState(null);
  const [auditChain, setAuditChain] = useState(null);

  useEffect(() => {
    api.getDashboard().then(setDashboard).catch(() => {});
    api.verifyAuditChain().then(setAuditChain).catch(() => {});
  }, []);

  const d = dashboard?.servers;
  const pieData = d ? [
    { name: 'Online', value: d.online },
    { name: 'Offline', value: d.offline },
    { name: 'Unreachable', value: d.unreachable },
    { name: 'Pending', value: d.pending_setup },
  ].filter(x => x.value > 0) : [];

  return (
    <div className="space-y-6">
      <h2 className="text-xl font-bold">Dashboard</h2>

      {/* Stats Cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {[
          { label: 'Total Servers', value: d?.total || 0, color: 'text-zinc-100' },
          { label: 'Online', value: d?.online || 0, color: 'text-emerald-400' },
          { label: 'Down', value: d?.offline || 0, color: d?.offline > 0 ? 'text-red-400' : 'text-zinc-400' },
          { label: 'Stale (7d+)', value: d?.stale || 0, color: d?.stale > 0 ? 'text-amber-400' : 'text-zinc-400' },
        ].map(s => (
          <div key={s.label} className="bg-zinc-900 border border-zinc-800 rounded-xl p-5">
            <p className="text-xs text-zinc-500 uppercase tracking-wider mb-1">{s.label}</p>
            <p className={`text-3xl font-bold ${s.color}`}>{s.value}</p>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* Pie Chart */}
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5">
          <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium mb-4">Server Status Distribution</h3>
          {pieData.length > 0 ? (
            <div className="flex items-center justify-center">
              <ResponsiveContainer width={200} height={200}>
                <PieChart>
                  <Pie data={pieData} dataKey="value" cx="50%" cy="50%" innerRadius={50} outerRadius={80} paddingAngle={3}>
                    {pieData.map((_, i) => <Cell key={i} fill={COLORS[i % COLORS.length]} />)}
                  </Pie>
                  <Tooltip contentStyle={{ background: '#18181b', border: '1px solid #27272a', borderRadius: 8, fontSize: 12 }} />
                </PieChart>
              </ResponsiveContainer>
              <div className="ml-4 space-y-2">
                {pieData.map((d, i) => (
                  <div key={d.name} className="flex items-center gap-2 text-xs">
                    <span className="w-3 h-3 rounded-full" style={{ background: COLORS[i % COLORS.length] }} />
                    <span className="text-zinc-400">{d.name}</span>
                    <span className="text-zinc-200 font-bold">{d.value}</span>
                  </div>
                ))}
              </div>
            </div>
          ) : <p className="text-zinc-600 text-sm text-center py-8">No server data yet.</p>}
        </div>

        {/* Server List Quick View */}
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
          <div className="px-5 py-3 border-b border-zinc-800">
            <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Servers</h3>
          </div>
          <div className="max-h-60 overflow-y-auto divide-y divide-zinc-800/50">
            {servers.map(s => (
              <button key={s.id} onClick={() => onSelectServer(s)}
                className="w-full flex items-center gap-3 px-5 py-2.5 hover:bg-zinc-800/40 transition-colors text-left">
                <span className={`w-2 h-2 rounded-full ${s.status === 'online' ? 'bg-emerald-400' : s.status === 'pending_setup' ? 'bg-zinc-500' : 'bg-red-400'}`} />
                <span className="text-sm text-zinc-200 flex-1">{s.name}</span>
                <span className="text-xs text-zinc-600 font-mono">{s.ip_address}</span>
                {s.last_ping_ms != null && <span className="text-xs text-zinc-500">{s.last_ping_ms.toFixed(0)}ms</span>}
              </button>
            ))}
            {servers.length === 0 && <div className="text-center py-8 text-zinc-600 text-sm">No servers added yet.</div>}
          </div>
        </div>
      </div>

      {/* Recent Activity */}
      <div className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
        <div className="px-5 py-3 border-b border-zinc-800 flex items-center justify-between">
          <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Recent Activity</h3>
          {auditChain && (
            <span className={`text-xs px-2 py-0.5 rounded-full ${auditChain.valid ? 'bg-emerald-500/20 text-emerald-400' : 'bg-red-500/20 text-red-400'}`}>
              Audit chain: {auditChain.valid ? 'valid' : 'BROKEN'}
            </span>
          )}
        </div>
        <div className="divide-y divide-zinc-800/50 max-h-64 overflow-y-auto">
          {(dashboard?.recent_activity || []).map((a, i) => (
            <div key={i} className="flex items-center gap-3 px-5 py-2.5">
              <span className="text-sm text-zinc-300 flex-1">{a.action?.replace(/_/g, ' ')}</span>
              <span className="text-xs text-zinc-600 truncate max-w-xs">{a.detail}</span>
              <span className="text-xs text-zinc-700 font-mono shrink-0">{timeAgo(a.created_at)}</span>
            </div>
          ))}
          {(!dashboard?.recent_activity || dashboard.recent_activity.length === 0) && (
            <div className="text-center py-8 text-zinc-600 text-sm">No activity yet.</div>
          )}
        </div>
      </div>
    </div>
  );
}

function timeAgo(ts) {
  if (!ts) return '';
  const diff = (Date.now() - new Date(ts)) / 1000;
  if (diff < 60) return `${Math.floor(diff)}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}
