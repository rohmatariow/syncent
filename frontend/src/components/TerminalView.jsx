import React, { useState, useEffect, useRef } from 'react';
import * as api from '../api/client';

export default function TerminalView({ server }) {
  const [status, setStatus] = useState('idle');
  const [totp, setTotp] = useState('');
  const [errorMsg, setErrorMsg] = useState('');
  const termRef = useRef(null);
  const wsRef = useRef(null);
  const xtermRef = useRef(null);

  const isGsocket = server.connection_mode === 'gsocket';

  const connect = async () => {
    if (!totp || totp.length !== 6) return;
    setStatus('connecting'); setErrorMsg('');

    try {
      const { Terminal } = await import('@xterm/xterm');
      const { FitAddon } = await import('@xterm/addon-fit');
      await import('@xterm/xterm/css/xterm.css');

      if (xtermRef.current) xtermRef.current.dispose();

      const term = new Terminal({
        cursorBlink: true, fontSize: 14,
        fontFamily: "'JetBrains Mono', 'Menlo', 'Courier New', monospace",
        theme: { background: '#09090b', foreground: '#e4e4e7', cursor: '#2dd4bf', selectionBackground: '#27272a' },
      });
      const fit = new FitAddon();
      term.loadAddon(fit);
      if (termRef.current) { termRef.current.innerHTML = ''; term.open(termRef.current); setTimeout(() => fit.fit(), 100); }
      xtermRef.current = term;

      const wsPath = isGsocket ? `/ws/gs-terminal/${server.id}` : `/ws/terminal/${server.id}`;
      const ws = new WebSocket(api.wsUrl(wsPath));
      wsRef.current = ws;

      ws.onmessage = (e) => {
        const data = e.data;
        try {
          const msg = JSON.parse(data);
          if (msg.type === 'auth_required') {
            ws.send(JSON.stringify({ type: 'auth', totp_code: totp }));
            setStatus('authenticating');
            return;
          }
          if (msg.type === 'connected') {
            setStatus('connected');
            setTimeout(() => { fit.fit(); ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows })); }, 200);
            return;
          }
          if (msg.type === 'error') { setErrorMsg(msg.message); setStatus('error'); return; }
          if (msg.type === 'pong') return;
        } catch {}
        term.write(data);
      };

      ws.onclose = () => { if (status !== 'error' && status !== 'idle') { term.write('\r\n\x1b[31m[Disconnected]\x1b[0m\r\n'); setStatus('idle'); } };
      ws.onerror = () => { setErrorMsg('WebSocket error.'); setStatus('error'); };

      term.onData(data => { if (ws.readyState === 1) ws.send(data); });

      const obs = new ResizeObserver(() => {
        fit.fit();
        if (ws.readyState === 1) ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
      });
      if (termRef.current) obs.observe(termRef.current);

    } catch (err) { setErrorMsg('Failed to load terminal: ' + err.message); setStatus('error'); }
  };

  const disconnect = () => { wsRef.current?.close(); xtermRef.current?.dispose(); xtermRef.current = null; setStatus('idle'); setTotp(''); };

  useEffect(() => () => { wsRef.current?.close(); xtermRef.current?.dispose(); }, [server.id]);

  if (server.status === 'pending_setup') {
    return <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-12 text-center"><p className="text-zinc-400">Server not set up. {isGsocket ? 'Test connection first.' : 'Inject SSH key first.'}</p></div>;
  }

  const inp = "bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2.5 text-sm text-zinc-200 outline-none focus:border-teal-500 text-center font-mono tracking-[0.5em] text-lg";

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <span className="text-xs text-zinc-500 uppercase tracking-wider font-medium">{isGsocket ? 'GSSocket' : 'SSH'} Terminal</span>
        <span className={`text-xs px-2 py-0.5 rounded-full ${status==='connected'?'bg-emerald-500/20 text-emerald-400':'bg-zinc-700/50 text-zinc-500'}`}>{status}</span>
      </div>

      {status === 'idle' && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-8">
          <p className="text-sm text-zinc-400 mb-4">Terminal requires TOTP verification.</p>
          <div className="flex items-center gap-3 max-w-sm">
            <input className={inp + ' flex-1'} value={totp} onChange={e => setTotp(e.target.value.replace(/\D/g,'').slice(0,6))} maxLength={6} placeholder="000000"/>
            <button onClick={connect} disabled={totp.length !== 6} className="px-6 py-2.5 bg-teal-500 hover:bg-teal-400 disabled:opacity-50 text-zinc-950 font-semibold rounded-lg text-sm">Connect</button>
          </div>
        </div>
      )}

      {(status === 'connecting' || status === 'authenticating') && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-12 text-center"><p className="text-zinc-400 text-sm">{status === 'authenticating' ? 'Verifying...' : 'Connecting...'}</p></div>
      )}

      {status === 'error' && (
        <div className="bg-zinc-900 border border-red-900/30 rounded-xl p-8 text-center">
          <p className="text-red-400 text-sm mb-4">{errorMsg}</p>
          <button onClick={() => { setStatus('idle'); setTotp(''); setErrorMsg(''); }} className="px-4 py-2 bg-zinc-800 text-zinc-300 rounded-lg text-sm">Try Again</button>
        </div>
      )}

      <div ref={termRef} className={`rounded-xl overflow-hidden ${status === 'connected' ? '' : 'hidden'}`} style={{ minHeight: '450px' }} />

      {status === 'connected' && (
        <div className="flex items-center gap-3">
          <button onClick={disconnect} className="px-4 py-2 bg-red-500/10 text-red-400 hover:bg-red-500/20 border border-red-500/20 rounded-lg text-sm">Disconnect</button>
        </div>
      )}
    </div>
  );
}
