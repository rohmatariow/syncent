import React, { useState } from 'react';
import * as api from '../api/client';

export default function AddServerModal({ onClose, onAdded, defaultMode = 'direct' }) {
  const [step, setStep] = useState('form');
  const [connMode, setConnMode] = useState(defaultMode);
  const [name, setName] = useState('');
  const [ip, setIp] = useState('');
  const [port, setPort] = useState('22');
  const [username, setUsername] = useState('root');
  const [gsocketSecret, setGsocketSecret] = useState('');
  const [location, setLocation] = useState('');
  const [tags, setTags] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [serverData, setServerData] = useState(null);
  const [sshPassword, setSshPassword] = useState('');
  const [setupMethod, setSetupMethod] = useState('manual');

  const handleCreate = async (e) => {
    e.preventDefault(); setError(''); setLoading(true);
    try {
      const payload = { name, connection_mode: connMode, location: location || undefined, tags: tags ? tags.split(',').map(t => t.trim()) : [] };
      if (connMode === 'direct') {
        payload.ip_address = ip;
        payload.ssh_port = parseInt(port);
        payload.ssh_username = username;
      } else {
        payload.gsocket_secret = gsocketSecret;
      }
      const res = await api.createServer(payload);
      setServerData(res);
      if (connMode === 'gsocket') {
        // Auto test connection for gsocket
        setStep('gsocket-test');
      } else {
        setStep('key');
      }
    } catch (err) { setError(err.message || 'Failed to add server.'); }
    setLoading(false);
  };

  const handleTestGSocket = async () => {
    setError(''); setLoading(true);
    try {
      await api.testConnection(serverData.id);
      onAdded();
    } catch (err) {
      setError(err.message || 'GSSocket connection failed. Check your secret and that gs-netcat is running on the server.');
    }
    setLoading(false);
  };

  const handleInjectKey = async () => {
    setError(''); setLoading(true);
    try { await api.injectKey(serverData.id, sshPassword); onAdded(); }
    catch (err) { setError(err.message || 'Key injection failed. Check SSH password.'); }
    setLoading(false);
  };

  const handleManualKey = async () => {
    setError(''); setLoading(true);
    try { await api.manualKey(serverData.id); onAdded(); }
    catch (err) { setError(err.message || 'Connection test failed.'); }
    setLoading(false);
  };

  const inp = "w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none focus:border-teal-500";

  return (
    <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-50 p-4" onClick={onClose}>
      <div className="bg-zinc-900 border border-zinc-700 rounded-2xl w-full max-w-lg p-6 shadow-2xl max-h-[90vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-lg font-semibold">
            {step === 'form' ? 'Add New Server' : step === 'gsocket-test' ? 'Test GSSocket Connection' : 'Install SSH Key'}
          </h2>
          <button onClick={onClose} className="text-zinc-500 hover:text-zinc-300 text-xl">×</button>
        </div>

        {error && <div className="bg-red-500/10 border border-red-500/20 text-red-400 text-sm rounded-lg p-3 mb-4">{error}</div>}

        {/* FORM */}
        {step === 'form' && (
          <form onSubmit={handleCreate} className="space-y-4">
            <div>
              <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Connection Mode</label>
              <div className="flex gap-2">
                {[['direct', 'Direct SSH'], ['gsocket', 'GSSocket']].map(([val, label]) => (
                  <button key={val} type="button" onClick={() => setConnMode(val)}
                    className={`flex-1 py-2 rounded-lg text-sm font-medium transition-all ${connMode === val ? 'bg-teal-500/20 text-teal-400 border border-teal-500/40' : 'bg-zinc-800 text-zinc-400 border border-zinc-700'}`}>{label}</button>
                ))}
              </div>
            </div>

            <div>
              <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Server Name</label>
              <input className={inp} value={name} onChange={e => setName(e.target.value)} placeholder="my-server" required />
            </div>

            {connMode === 'direct' && (<>
              <div className="grid grid-cols-2 gap-3">
                <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">IP Address</label>
                  <input className={`${inp} font-mono`} value={ip} onChange={e => setIp(e.target.value)} placeholder="0.0.0.0" required /></div>
                <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">SSH Port</label>
                  <input className={`${inp} font-mono`} value={port} onChange={e => setPort(e.target.value)} /></div>
              </div>
              <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">SSH Username</label>
                <input className={inp} value={username} onChange={e => setUsername(e.target.value)} required /></div>
            </>)}

            {connMode === 'gsocket' && (<>
              <div className="bg-zinc-800 rounded-lg p-3">
                <p className="text-xs text-zinc-400">For servers behind NAT/firewall. Your server must already have gs-netcat listening.</p>
              </div>
              <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">GSSocket Secret</label>
                <input className={`${inp} font-mono`} value={gsocketSecret} onChange={e => setGsocketSecret(e.target.value)} placeholder="Your gs-netcat secret" required /></div>
            </>)}

            <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Location</label>
              <input className={inp} value={location} onChange={e => setLocation(e.target.value)} placeholder="Singapore, Tokyo..." /></div>

            <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Tags (comma separated)</label>
              <input className={inp} value={tags} onChange={e => setTags(e.target.value)} placeholder="production, web" /></div>

            <button disabled={loading} className="w-full py-2.5 bg-teal-500 hover:bg-teal-400 disabled:opacity-50 text-zinc-950 font-semibold rounded-lg text-sm">
              {loading ? 'Creating...' : 'Add Server'}
            </button>
          </form>
        )}

        {/* GSOCKET TEST */}
        {step === 'gsocket-test' && serverData && (
          <div className="space-y-4">
            <div className="bg-zinc-800 rounded-lg p-4">
              <p className="text-xs text-zinc-400">Server <strong className="text-zinc-200">{name}</strong> added. Now testing GSSocket connection...</p>
            </div>
            <button onClick={handleTestGSocket} disabled={loading}
              className="w-full py-2.5 bg-teal-500 hover:bg-teal-400 disabled:opacity-50 text-zinc-950 font-semibold rounded-lg text-sm">
              {loading ? 'Testing connection...' : 'Test Connection'}
            </button>
            <button onClick={onAdded} className="w-full text-xs text-zinc-600 hover:text-zinc-400">Skip test, add anyway</button>
          </div>
        )}

        {/* SSH KEY SETUP */}
        {step === 'key' && serverData && (
          <div className="space-y-4">
            <div className="bg-zinc-800 rounded-lg p-4">
              <p className="text-xs text-zinc-400 mb-2">Public key for <strong className="text-zinc-200">{name}</strong>:</p>
              <code className="text-xs text-teal-400 break-all select-all block bg-zinc-900 p-3 rounded">{serverData.public_key}</code>
            </div>
            <div className="bg-zinc-800 rounded-lg p-3">
              <p className="text-xs text-zinc-400 mb-2">Add to your server:</p>
              <code className="text-xs text-emerald-400 select-all block">{serverData.message}</code>
            </div>

            <div className="flex gap-2">
              {[['manual', 'Manual Copy'], ['inject', 'Password Inject']].map(([val, label]) => (
                <button key={val} onClick={() => setSetupMethod(val)}
                  className={`flex-1 py-2 rounded-lg text-sm font-medium transition-all ${setupMethod === val ? 'bg-teal-500/20 text-teal-400 border border-teal-500/40' : 'bg-zinc-800 text-zinc-400 border border-zinc-700'}`}>{label}</button>
              ))}
            </div>

            {setupMethod === 'manual' && (
              <button onClick={handleManualKey} disabled={loading} className="w-full py-2.5 bg-teal-500 hover:bg-teal-400 disabled:opacity-50 text-zinc-950 font-semibold rounded-lg text-sm">
                {loading ? 'Testing...' : "I've copied the key — Test Connection"}
              </button>
            )}

            {setupMethod === 'inject' && (
              <div className="space-y-3">
                <p className="text-xs text-zinc-500">Enter SSH password once. It will NOT be stored.</p>
                <input type="password" className={inp} value={sshPassword} onChange={e => setSshPassword(e.target.value)} placeholder="SSH password" />
                <button onClick={handleInjectKey} disabled={loading || !sshPassword} className="w-full py-2.5 bg-teal-500 hover:bg-teal-400 disabled:opacity-50 text-zinc-950 font-semibold rounded-lg text-sm">
                  {loading ? 'Injecting...' : 'Inject Key & Connect'}
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
