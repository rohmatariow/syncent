import React, { useState, useEffect } from 'react';
import * as api from '../api/client';

const ACTION_ICONS = {
  server_added: '➕', server_removed: '❌', key_injected: '🔑', manual_key_verified: '🔑',
  command_executed: '⚡', terminal_opened: '💻', terminal_closed: '💻',
  login_success: '🔓', login_failed: '🔒', config_exported: '📦', status_changed: '🔄',
};

export default function HistoryPanel({ server }) {
  const [history, setHistory] = useState([]);
  const [statusHistory, setStatusHistory] = useState([]);
  const [statusRange, setStatusRange] = useState('7d');
  const [page, setPage] = useState(1);
  const [tab, setTab] = useState('activity');

  useEffect(() => {
    api.getServerHistory(server.id, page, 30).then(setHistory).catch(() => {});
    api.getStatusHistory(server.id, statusRange).then(d => setStatusHistory(d.transitions || [])).catch(() => {});
  }, [server.id, page, statusRange]);

  return (
    <div className="space-y-6">
      {/* Tab toggle */}
      <div className="flex gap-2">
        <button onClick={() => setTab('activity')}
          className={`px-4 py-1.5 rounded-md text-sm font-medium ${tab === 'activity' ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-500 hover:text-zinc-300'}`}>
          Activity
        </button>
        <button onClick={() => setTab('uptime')}
          className={`px-4 py-1.5 rounded-md text-sm font-medium ${tab === 'uptime' ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-500 hover:text-zinc-300'}`}>
          Uptime History
        </button>
      </div>

      {/* Activity Log */}
      {tab === 'activity' && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
          <div className="px-5 py-3 border-b border-zinc-800">
            <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Activity Log</h3>
          </div>
          {history.length === 0 ? (
            <div className="text-center py-12 text-zinc-600 text-sm">No activity recorded yet.</div>
          ) : (
            <div className="divide-y divide-zinc-800/50">
              {history.map((entry, i) => (
                <div key={i} className="flex items-start gap-3 py-3 px-5 hover:bg-zinc-800/20 transition-colors">
                  <span className="text-sm mt-0.5">{ACTION_ICONS[entry.action] || '📋'}</span>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm text-zinc-300 font-medium">{formatAction(entry.action)}</span>
                      <span className="text-xs text-zinc-600 font-mono">{formatTime(entry.created_at)}</span>
                    </div>
                    {entry.detail && <p className="text-xs text-zinc-500 mt-0.5 truncate">{entry.detail}</p>}
                  </div>
                  {entry.source_ip && <span className="text-xs text-zinc-700 font-mono shrink-0">{entry.source_ip}</span>}
                </div>
              ))}
            </div>
          )}
          {history.length >= 30 && (
            <div className="px-5 py-3 border-t border-zinc-800 flex gap-2">
              <button onClick={() => setPage(p => Math.max(1, p - 1))} disabled={page === 1}
                className="px-3 py-1 bg-zinc-800 text-zinc-400 rounded text-xs disabled:opacity-30">← Prev</button>
              <span className="text-xs text-zinc-600 py-1">Page {page}</span>
              <button onClick={() => setPage(p => p + 1)}
                className="px-3 py-1 bg-zinc-800 text-zinc-400 rounded text-xs">Next →</button>
            </div>
          )}
        </div>
      )}

      {/* Uptime History */}
      {tab === 'uptime' && (
        <div className="space-y-4">
          <div className="flex gap-2">
            {['24h', '7d', '30d'].map(r => (
              <button key={r} onClick={() => setStatusRange(r)}
                className={`px-3 py-1 rounded-md text-xs font-medium ${statusRange === r ? 'bg-zinc-800 text-zinc-200' : 'text-zinc-500 hover:text-zinc-300'}`}>
                {r}
              </button>
            ))}
          </div>

          <div className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
            {/* Visual timeline bar */}
            <div className="px-5 py-4">
              <div className="h-8 bg-zinc-800 rounded-full overflow-hidden flex">
                {statusHistory.length === 0 ? (
                  <div className={`h-full flex-1 ${server.status === 'online' ? 'bg-emerald-500/60' : 'bg-red-500/40'}`} />
                ) : (
                  statusHistory.map((t, i) => {
                    const next = statusHistory[i + 1];
                    const start = new Date(t.changed_at).getTime();
                    const end = next ? new Date(next.changed_at).getTime() : Date.now();
                    const total = Date.now() - new Date(statusHistory[0]?.changed_at || Date.now()).getTime();
                    const width = total > 0 ? ((end - start) / total) * 100 : 100;
                    return (
                      <div key={i}
                        className={`h-full ${t.status === 'online' ? 'bg-emerald-500/60' : 'bg-red-500/40'}`}
                        style={{ width: `${Math.max(width, 0.5)}%` }}
                        title={`${t.status} at ${formatTime(t.changed_at)}`}
                      />
                    );
                  })
                )}
              </div>
              <div className="flex justify-between mt-1 text-xs text-zinc-600">
                <span>{statusRange} ago</span>
                <span>now</span>
              </div>
            </div>

            {/* Transition list */}
            <div className="border-t border-zinc-800 divide-y divide-zinc-800/50">
              {statusHistory.length === 0 ? (
                <div className="text-center py-8 text-zinc-600 text-sm">No status transitions in this period.</div>
              ) : (
                statusHistory.map((t, i) => (
                  <div key={i} className="flex items-center gap-3 px-5 py-2.5">
                    <span className={`w-2.5 h-2.5 rounded-full ${t.status === 'online' ? 'bg-emerald-400' : 'bg-red-400'}`} />
                    <span className="text-sm text-zinc-300">{t.status}</span>
                    <span className="text-xs text-zinc-600 font-mono ml-auto">{formatTime(t.changed_at)}</span>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function formatAction(action) {
  return action.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function formatTime(ts) {
  if (!ts) return '';
  const d = new Date(ts);
  const now = new Date();
  const diff = (now - d) / 1000;
  if (diff < 60) return `${Math.floor(diff)}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}
