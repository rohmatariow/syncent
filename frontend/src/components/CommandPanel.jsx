import React, { useState, useEffect } from 'react';
import * as api from '../api/client';

const TIER_STYLES = {
  safe: 'bg-emerald-500/20 text-emerald-400',
  dangerous: 'bg-amber-500/20 text-amber-400',
  blocked: 'bg-red-500/20 text-red-400',
};

export default function CommandPanel({ server, tfaRequired = true }) {
  const [command, setCommand] = useState('');
  const [totp, setTotp] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [timeout, setTimeout_] = useState(30);
  const [result, setResult] = useState(null);
  const [tier, setTier] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [snippets, setSnippets] = useState([]);
  const [showAddSnippet, setShowAddSnippet] = useState(false);
  const [snippetName, setSnippetName] = useState('');
  const [snippetCategory, setSnippetCategory] = useState('custom');

  const [tfa, setTfa] = useState(tfaRequired);
  useEffect(() => { api.listSnippets().then(setSnippets).catch(() => {}); api.get2FAStatus().then(d => setTfa(d.required)).catch(() => {}); }, []);

  // Auto-classify on command change
  useEffect(() => {
    if (!command.trim()) { setTier(null); return; }
    const t = setTimeout(async () => {
      try {
        const res = await api.classifyCommand(server.id, command);
        setTier(res);
      } catch {}
    }, 300);
    return () => clearTimeout(t);
  }, [command, server.id]);

  const handleExecute = async () => {
    setError(''); setResult(null); setLoading(true);
    try {
      const res = await api.executeCommand(server.id, command, timeout, tfa ? (totp || undefined) : '', tfa ? '' : (confirmation || undefined));
      setResult(res);
      setTotp('');
    } catch (err) {
      if (err.code === 'TOTP_REQUIRED') {
        setError('This command requires TOTP verification.');
      } else if (err.code === 'CONFIRM_REQUIRED') {
        setError('Type EXECUTE to confirm this dangerous command.');
      } else if (err.code === 'COMMAND_BLOCKED') {
        setError('Command blocked — too destructive for dashboard.');
      } else {
        setError(err.message || 'Execution failed.');
      }
    }
    setLoading(false);
  };

  const runSnippet = async (snippet) => {
    setCommand(snippet.command);
    setError(''); setResult(null); setLoading(true);
    try {
      const res = await api.executeCommand(server.id, snippet.command, 30, snippet.is_dangerous ? totp : undefined);
      setResult(res);
    } catch (err) {
      if (err.code === 'TOTP_REQUIRED') {
        setError(`Snippet "${snippet.name}" requires TOTP. Enter code and try again.`);
        setCommand(snippet.command);
      } else {
        setError(err.message || 'Failed.');
      }
    }
    setLoading(false);
  };

  const handleSaveSnippet = async () => {
    if (!snippetName || !command) return;
    try {
      await api.createSnippet(snippetName, command, snippetCategory);
      setSnippets(await api.listSnippets());
      setShowAddSnippet(false);
      setSnippetName('');
    } catch {}
  };

  const handleDeleteSnippet = async (id) => {
    await api.deleteSnippet(id);
    setSnippets(await api.listSnippets());
  };

  return (
    <div className="space-y-6">
      {/* Command Input */}
      <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 space-y-4">
        <div className="flex items-center gap-3">
          <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Execute Command</h3>
          {tier && (
            <span className={`text-xs px-2 py-0.5 rounded-full ${TIER_STYLES[tier.tier]}`}>
              {tier.tier}
            </span>
          )}
        </div>

        <div className="flex gap-2">
          <input className="flex-1 bg-zinc-800 border border-zinc-700 rounded-lg px-4 py-2.5 text-sm text-zinc-200 outline-none focus:border-teal-500 font-mono"
            value={command} onChange={e => setCommand(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && !tier?.blocked && handleExecute()}
            placeholder="uptime, df -h, systemctl status nginx..." />
          <button onClick={handleExecute} disabled={loading || !command.trim() || tier?.blocked}
            className="px-5 py-2.5 bg-teal-500 hover:bg-teal-400 disabled:opacity-50 text-zinc-950 font-semibold rounded-lg text-sm shrink-0">
            {loading ? '...' : 'Run'}
          </button>
        </div>

        {tier?.tier === 'dangerous' && (
          <div className="flex items-center gap-2">
            {tfa ? (<>
              <span className="text-xs text-amber-400">TOTP required:</span>
              <input className="bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-zinc-200 outline-none focus:border-teal-500 font-mono tracking-widest w-32 text-center"
                value={totp} onChange={e => setTotp(e.target.value.replace(/\D/g, '').slice(0, 6))} maxLength={6} placeholder="000000" />
            </>) : (<>
              <span className="text-xs text-amber-400">Type EXECUTE to confirm:</span>
              <input className="bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-zinc-200 outline-none focus:border-teal-500 w-32"
                value={confirmation} onChange={e => setConfirmation(e.target.value)} placeholder="EXECUTE" />
            </>)}
          </div>
        )}

        {error && <div className="bg-red-500/10 border border-red-500/20 text-red-400 text-sm rounded-lg p-3">{error}</div>}
      </div>

      {/* Result */}
      {result && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
          <div className="px-4 py-2.5 border-b border-zinc-800 flex items-center gap-3">
            <span className={`text-xs px-2 py-0.5 rounded-full ${result.exit_code === 0 ? 'bg-emerald-500/20 text-emerald-400' : 'bg-red-500/20 text-red-400'}`}>
              exit {result.exit_code}
            </span>
            <span className="text-xs text-zinc-600">{result.duration_ms}ms</span>
            <span className={`text-xs px-2 py-0.5 rounded-full ${TIER_STYLES[result.command_tier]}`}>{result.command_tier}</span>
            <button onClick={() => setShowAddSnippet(true)} className="ml-auto text-xs text-zinc-500 hover:text-teal-400">Save as snippet</button>
          </div>
          {result.stdout && (
            <pre className="px-4 py-3 text-xs text-zinc-300 font-mono whitespace-pre-wrap overflow-x-auto max-h-96">{result.stdout}</pre>
          )}
          {result.stderr && (
            <pre className="px-4 py-3 text-xs text-red-400 font-mono whitespace-pre-wrap border-t border-zinc-800">{result.stderr}</pre>
          )}
        </div>
      )}

      {/* Save Snippet Modal */}
      {showAddSnippet && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 space-y-3">
          <h4 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Save as Snippet</h4>
          <input className="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none focus:border-teal-500"
            value={snippetName} onChange={e => setSnippetName(e.target.value)} placeholder="Snippet name" />
          <select className="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none"
            value={snippetCategory} onChange={e => setSnippetCategory(e.target.value)}>
            <option value="system">System</option><option value="web">Web</option><option value="database">Database</option><option value="docker">Docker</option><option value="custom">Custom</option>
          </select>
          <div className="flex gap-2">
            <button onClick={handleSaveSnippet} className="px-4 py-2 bg-teal-500 text-zinc-950 rounded-lg text-sm font-semibold">Save</button>
            <button onClick={() => setShowAddSnippet(false)} className="px-4 py-2 bg-zinc-800 text-zinc-400 rounded-lg text-sm">Cancel</button>
          </div>
        </div>
      )}

      {/* Snippets */}
      {snippets.length > 0 && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
          <div className="px-5 py-3 border-b border-zinc-800">
            <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Saved Snippets</h3>
          </div>
          <div className="divide-y divide-zinc-800/50">
            {snippets.map(s => (
              <div key={s.id} className="flex items-center gap-3 px-5 py-3 hover:bg-zinc-800/30 transition-colors">
                <div className="flex-1 min-w-0">
                  <div className="text-sm text-zinc-200 font-medium">{s.name}</div>
                  <code className="text-xs text-zinc-500 font-mono">{s.command}</code>
                </div>
                <span className="text-xs text-zinc-600">{s.category}</span>
                {s.is_dangerous && <span className="text-xs px-2 py-0.5 rounded-full bg-amber-500/20 text-amber-400">TOTP</span>}
                <button onClick={() => runSnippet(s)} className="px-3 py-1 bg-zinc-800 hover:bg-zinc-700 text-zinc-300 text-xs rounded-lg">Run</button>
                <button onClick={() => handleDeleteSnippet(s.id)} className="text-zinc-600 hover:text-red-400 text-xs">×</button>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
