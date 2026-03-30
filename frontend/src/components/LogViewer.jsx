import React, { useState, useEffect, useRef } from 'react';
import { wsUrl } from '../api/client';

const LEVEL_COLORS = { INFO: 'text-zinc-500', WARN: 'text-amber-400', ERROR: 'text-red-400', DEBUG: 'text-zinc-600' };

export default function LogViewer({ server }) {
  const [lines, setLines] = useState([]);
  const [connected, setConnected] = useState(false);
  const [paused, setPaused] = useState(false);
  const [filter, setFilter] = useState('ALL');
  const [search, setSearch] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);
  const wsRef = useRef(null);
  const bottomRef = useRef(null);

  const isGsocket = server.connection_mode === 'gsocket';

  useEffect(() => {
    setLines([]); setConnected(false);
    const wsPath = isGsocket ? `/ws/gs-logs/${server.id}` : `/ws/logs/${server.id}`;
    const ws = new WebSocket(wsUrl(wsPath));
    wsRef.current = ws;

    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);
    ws.onerror = () => setConnected(false);

    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data);
        if (msg.type === 'log_line') {
          setLines(prev => {
            const next = [...prev, msg.data];
            return next.length > 2000 ? next.slice(-1500) : next;
          });
        }
      } catch {}
    };

    return () => { ws.close(); };
  }, [server.id]);

  useEffect(() => {
    if (autoScroll && bottomRef.current) bottomRef.current.scrollIntoView({ behavior: 'smooth' });
  }, [lines, autoScroll]);

  const togglePause = () => {
    if (wsRef.current?.readyState === 1) {
      wsRef.current.send(JSON.stringify({ type: paused ? 'resume' : 'pause' }));
      setPaused(!paused);
    }
  };

  const filtered = lines.filter(l => {
    if (filter !== 'ALL' && l.level !== filter) return false;
    if (search && !l.message.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 flex-wrap">
        {['ALL', 'INFO', 'WARN', 'ERROR'].map(level => (
          <button key={level} onClick={() => setFilter(level)}
            className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
              filter === level
                ? level === 'ERROR' ? 'bg-red-500/20 text-red-400'
                : level === 'WARN' ? 'bg-amber-500/20 text-amber-400'
                : 'bg-zinc-800 text-zinc-200'
                : 'text-zinc-500 hover:text-zinc-300'
            }`}>{level}</button>
        ))}
        <input className="bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1 text-xs text-zinc-200 outline-none focus:border-teal-500 w-48 ml-auto" placeholder="Search logs..." value={search} onChange={e => setSearch(e.target.value)} />
        <button onClick={togglePause} className={`px-3 py-1 rounded-md text-xs font-medium ${paused ? 'bg-amber-500/20 text-amber-400' : 'bg-zinc-800 text-zinc-400'}`}>
          {paused ? 'Resume' : 'Pause'}
        </button>
        <button onClick={() => setAutoScroll(!autoScroll)} className={`px-3 py-1 rounded-md text-xs font-medium ${autoScroll ? 'bg-teal-500/20 text-teal-400' : 'bg-zinc-800 text-zinc-400'}`}>
          {autoScroll ? 'Auto' : 'Manual'}
        </button>
        <span className={`text-xs px-2 py-0.5 rounded-full ${connected ? 'bg-emerald-500/20 text-emerald-400' : 'bg-red-500/20 text-red-400'}`}>
          {connected ? 'live' : 'disconnected'}
        </span>
        <span className="text-xs text-zinc-600">{filtered.length} lines</span>
      </div>

      <div className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
        <div className="h-[60vh] overflow-y-auto font-mono text-xs">
          {filtered.length === 0 ? (
            <div className="text-center py-12 text-zinc-600">
              {connected ? 'Waiting for log data...' : `Not connected. ${isGsocket ? 'Check GSSocket connection.' : 'Check server status.'}`}
            </div>
          ) : (
            filtered.map((line, i) => (
              <div key={i} className="flex gap-3 py-1.5 px-3 border-b border-zinc-800/30 hover:bg-zinc-800/30 transition-colors">
                <span className="text-zinc-600 shrink-0 w-36">{line.timestamp?.split('T')[1]?.split('.')[0] || line.timestamp}</span>
                <span className={`shrink-0 w-12 font-semibold ${LEVEL_COLORS[line.level] || 'text-zinc-400'}`}>{line.level}</span>
                <span className="text-zinc-300 break-all">{line.message}</span>
              </div>
            ))
          )}
          <div ref={bottomRef} />
        </div>
      </div>
    </div>
  );
}
