import React, { useState, useEffect, useRef } from 'react';
import * as api from '../api/client';

export default function AppSettings({ onRefresh }) {
  const [tab, setTab] = useState('password');
  return (
    <div className="space-y-6 max-w-2xl">
      <h2 className="text-xl font-bold">Settings</h2>
      <div className="flex gap-2">
        {['password','config','backup'].map(t => (
          <button key={t} onClick={() => setTab(t)} className={`px-4 py-1.5 rounded-md text-sm font-medium capitalize ${tab===t?'bg-zinc-800 text-zinc-100':'text-zinc-500 hover:text-zinc-300'}`}>{t}</button>
        ))}
      </div>
      {tab==='password'&&<PasswordReset/>}
      {tab==='config'&&<AppConfig/>}
      {tab==='backup'&&<BackupRestore onRefresh={onRefresh}/>}
    </div>
  );
}

function PasswordReset() {
  const [cur, setCur] = useState(''); const [nw, setNw] = useState('');
  const [confirm, setConfirm] = useState(''); const [totp, setTotp] = useState('');
  const [msg, setMsg] = useState(''); const [error, setError] = useState(''); const [loading, setLoading] = useState(false);
  const handle = async (e) => {
    e.preventDefault(); setError(''); setMsg('');
    if (nw !== confirm) { setError('Passwords do not match.'); return; }
    if (nw.length < 12) { setError('Min 12 characters.'); return; }
    setLoading(true);
    try { await api.resetPassword(cur, nw, totp); setMsg('Password changed.'); setCur(''); setNw(''); setConfirm(''); setTotp(''); }
    catch (err) { setError(err.message || 'Failed.'); }
    setLoading(false);
  };
  const inp = "w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none focus:border-teal-500";
  return (
    <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5">
      <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium mb-4">Change Password</h3>
      {msg&&<div className="bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 text-sm rounded-lg p-3 mb-4">{msg}</div>}
      {error&&<div className="bg-red-500/10 border border-red-500/20 text-red-400 text-sm rounded-lg p-3 mb-4">{error}</div>}
      <form onSubmit={handle} className="space-y-3">
        <div><label className="text-xs text-zinc-500 block mb-1">Current Password</label><input type="password" className={inp} value={cur} onChange={e => setCur(e.target.value)} required/></div>
        <div><label className="text-xs text-zinc-500 block mb-1">New Password (min 12)</label><input type="password" className={inp} value={nw} onChange={e => setNw(e.target.value)} minLength={12} required/></div>
        <div><label className="text-xs text-zinc-500 block mb-1">Confirm</label><input type="password" className={inp} value={confirm} onChange={e => setConfirm(e.target.value)} required/></div>
        <div><label className="text-xs text-zinc-500 block mb-1">TOTP Code</label><input className={`${inp} text-center font-mono tracking-widest w-40`} value={totp} onChange={e => setTotp(e.target.value.replace(/\D/g,'').slice(0,6))} maxLength={6} placeholder="000000" required/></div>
        <button disabled={loading} className="px-4 py-2 bg-teal-500 hover:bg-teal-400 disabled:opacity-50 text-zinc-950 font-semibold rounded-lg text-sm">{loading?'Changing...':'Change Password'}</button>
      </form>
    </div>
  );
}

function AppConfig() {
  const [config, setConfig] = useState({}); const [loading, setLoading] = useState(true); const [msg, setMsg] = useState('');
  const [tfa, setTfa] = useState(true); const [show2FAModal, setShow2FAModal] = useState(false);

  useEffect(() => {
    api.getAppConfig().then(c => { setConfig(c); setLoading(false); }).catch(() => setLoading(false));
    api.get2FAStatus().then(d => setTfa(d.required)).catch(() => {});
  }, []);

  const toggle = async (key) => { const nv = config[key]==='true'?'false':'true'; setConfig({...config,[key]:nv}); await api.updateAppConfig({[key]:nv}); setMsg('Saved.'); setTimeout(()=>setMsg(''),2000); };
  const save = async () => { await api.updateAppConfig(config); setMsg('Saved.'); setTimeout(()=>setMsg(''),2000); };

  if (loading) return <p className="text-zinc-500">Loading...</p>;
  return (
    <div className="space-y-4">
      <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 space-y-4">
        <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">App Configuration</h3>
        {msg&&<div className="text-emerald-400 text-sm">{msg}</div>}
        <Toggle label="Allow Registration" desc="Show register option on login page" value={config.allow_registration==='true'} onToggle={()=>toggle('allow_registration')}/>
        <NumRow label="Max Servers" desc="Max monitored servers per user" value={config.max_servers||'50'} onChange={v=>setConfig({...config,max_servers:v})}/>
        <NumRow label="Health Check Interval" desc="Default seconds between checks" value={config.health_check_interval_default||'30'} onChange={v=>setConfig({...config,health_check_interval_default:v})}/>
        <NumRow label="Log Retention (days)" desc="Days to keep metric history" value={config.log_retention_days||'30'} onChange={v=>setConfig({...config,log_retention_days:v})}/>
        <button onClick={save} className="px-4 py-2 bg-teal-500 hover:bg-teal-400 text-zinc-950 font-semibold rounded-lg text-sm">Save Configuration</button>
      </div>

      <div className={`bg-zinc-900 border rounded-xl p-5 space-y-4 ${tfa ? 'border-zinc-800' : 'border-amber-900/30'}`}>
        <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Two-Factor Authentication</h3>
        <div className="flex items-center justify-between py-2">
          <div>
            <p className="text-sm text-zinc-200">Require 2FA</p>
            <p className="text-xs text-zinc-500">{tfa ? 'TOTP required for sensitive actions' : 'Text confirmation used instead'}</p>
          </div>
          <span className={`text-xs px-2.5 py-1 rounded-full font-medium ${tfa ? 'bg-emerald-500/20 text-emerald-400' : 'bg-amber-500/20 text-amber-400'}`}>{tfa ? 'Enabled' : 'Disabled'}</span>
        </div>
        <button onClick={() => setShow2FAModal(true)}
          className={`px-4 py-2 rounded-lg text-sm font-medium ${tfa ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20' : 'bg-teal-500/10 text-teal-400 border border-teal-500/20'}`}>
          {tfa ? 'Disable 2FA' : 'Enable 2FA'}
        </button>
        {!tfa && <p className="text-xs text-amber-400">Without 2FA, sensitive actions use text confirmation only.</p>}
      </div>

      {show2FAModal && <TwoFAModal enabled={tfa} onClose={() => setShow2FAModal(false)} onDone={(v) => { setTfa(v); setShow2FAModal(false); }} />}
    </div>
  );
}

function TwoFAModal({ enabled, onClose, onDone }) {
  const [totp, setTotp] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const expected = enabled ? 'DISABLE 2FA' : 'ENABLE 2FA';

  const handle = async (e) => {
    e.preventDefault(); setError(''); setLoading(true);
    if (confirmation !== expected) { setError(`Type "${expected}" to confirm.`); setLoading(false); return; }
    if (enabled && totp.length !== 6) { setError('Enter TOTP code.'); setLoading(false); return; }
    try {
      await api.toggle2FA(!enabled, enabled ? totp : '', confirmation);
      onDone(!enabled);
    } catch (err) { setError(err.message || 'Failed.'); }
    setLoading(false);
  };

  const inp = "w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none focus:border-teal-500";
  return (
    <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-50" onClick={onClose}>
      <div className="bg-zinc-900 border border-zinc-700 rounded-2xl w-full max-w-md p-6 shadow-2xl" onClick={e => e.stopPropagation()}>
        <h3 className="text-lg font-semibold mb-2">{enabled ? 'Disable' : 'Enable'} 2FA</h3>
        <p className="text-xs text-zinc-400 mb-4">{enabled ? 'Text confirmation will replace TOTP for sensitive actions.' : 'TOTP will be required for sensitive actions.'}</p>
        {error && <div className="bg-red-500/10 border border-red-500/20 text-red-400 text-sm rounded-lg p-3 mb-4">{error}</div>}
        <form onSubmit={handle} className="space-y-4">
          {enabled && <div><label className="text-xs text-zinc-500 block mb-1">TOTP Code</label>
            <input className={`${inp} text-center font-mono tracking-widest w-40`} value={totp} onChange={e => setTotp(e.target.value.replace(/\D/g,'').slice(0,6))} maxLength={6} placeholder="000000" autoFocus /></div>}
          <div><label className="text-xs text-zinc-500 block mb-1">Type <code className="text-amber-400">{expected}</code></label>
            <input className={inp} value={confirmation} onChange={e => setConfirmation(e.target.value)} placeholder={expected} autoFocus={!enabled} /></div>
          <div className="flex gap-2">
            <button disabled={loading || confirmation !== expected} className={`flex-1 py-2 font-semibold rounded-lg text-sm disabled:opacity-50 ${enabled ? 'bg-amber-500 text-zinc-950' : 'bg-teal-500 text-zinc-950'}`}>{loading ? '...' : enabled ? 'Disable 2FA' : 'Enable 2FA'}</button>
            <button type="button" onClick={onClose} className="flex-1 py-2 bg-zinc-800 text-zinc-400 rounded-lg text-sm">Cancel</button>
          </div>
        </form>
      </div>
    </div>
  );
}


function Toggle({label,desc,value,onToggle}) {
  return <div className="flex items-center justify-between py-2">
    <div><p className="text-sm text-zinc-200">{label}</p><p className="text-xs text-zinc-500">{desc}</p></div>
    <button onClick={onToggle} className={`w-12 h-6 rounded-full transition-colors relative ${value?'bg-teal-500':'bg-zinc-700'}`}><span className={`absolute top-0.5 w-5 h-5 rounded-full bg-white transition-transform ${value?'left-6':'left-0.5'}`}/></button>
  </div>;
}
function NumRow({label,desc,value,onChange}) {
  return <div className="flex items-center justify-between py-2">
    <div><p className="text-sm text-zinc-200">{label}</p><p className="text-xs text-zinc-500">{desc}</p></div>
    <input className="bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none focus:border-teal-500 w-24 text-center" value={value} onChange={e=>onChange(e.target.value)} type="number" min="1"/>
  </div>;
}

function maskKey(key) {
  if (!key || key.length < 20) return '****';
  return key.slice(0, 8) + '...' + key.slice(-8);
}

function BackupRestore({ onRefresh }) {
  const [msg, setMsg] = useState(''); const [error, setError] = useState('');
  const [exportWithKeys, setExportWithKeys] = useState(false);
  const [exportTotp, setExportTotp] = useState(''); const [exportPassword, setExportPassword] = useState('');
  const [exporting, setExporting] = useState(false);
  const [importFile, setImportFile] = useState(null); const [importFileName, setImportFileName] = useState('');
  const [importWithKeys, setImportWithKeys] = useState(false); const [importPassword, setImportPassword] = useState('');
  const [importing, setImporting] = useState(false); const [importPreview, setImportPreview] = useState(null);
  const [importError, setImportError] = useState('');
  const fileRef = useRef(null);

  const handleExport = async () => {
    setError(''); setMsg(''); setExporting(true);
    if (exportWithKeys && (exportTotp.length !== 6 || !exportPassword)) { setError('TOTP and export password required.'); setExporting(false); return; }
    try {
      const data = await api.exportConfig(exportWithKeys, exportTotp, exportPassword);
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const a = document.createElement('a'); a.href = URL.createObjectURL(blob);
      a.download = `syncent-backup-${new Date().toISOString().slice(0,10)}.json`;
      a.click(); URL.revokeObjectURL(a.href);
      setMsg(`Exported ${data.count} servers${exportWithKeys ? ' (with keys)' : ''}.`);
      setExportTotp('');
    } catch (err) { setError(err.message || 'Export failed.'); }
    setExporting(false);
  };

  const handleFileSelect = async (e) => {
    const file = e.target.files[0]; if (!file) return;
    setImportError(''); setImportPreview(null);
    if (!file.name.endsWith('.json')) { setImportError('Only .json files accepted.'); setImportFile(null); setImportFileName(''); return; }
    if (file.size > 10 * 1024 * 1024) { setImportError('File too large (max 10MB).'); return; }
    try {
      const text = await file.text();
      let data;
      try { data = JSON.parse(text); } catch { setImportError('Invalid JSON: file could not be parsed.'); setImportFile(null); setImportFileName(''); return; }

      const servers = data.servers || data;
      if (!Array.isArray(servers)) { setImportError('Invalid format: root must contain a "servers" array. Expected: {"servers": [...]}'); return; }
      if (servers.length === 0) { setImportError('Empty servers array. Nothing to import.'); return; }

      // Validate each server
      const errors = [];
      servers.forEach((s, i) => {
        if (!s.name) errors.push(`Server #${i+1}: missing "name"`);
        const mode = s.connection_mode || 'direct';
        if (mode === 'gsocket') {
          if (!s.gsocket_secret && !s.encrypted_gsocket_secret) errors.push(`Server #${i+1} "${s.name||'?'}": missing gsocket_secret`);
        } else {
          if (!s.ip_address) errors.push(`Server #${i+1} "${s.name||'?'}": missing "ip_address"`);
          if (!s.ssh_username) errors.push(`Server #${i+1} "${s.name||'?'}": missing "ssh_username"`);
        }
      });
      if (errors.length > 0) { setImportError('Format errors:\n' + errors.join('\n')); return; }

      const hasKeys = servers.some(s => s.encrypted_key);
      setImportWithKeys(hasKeys);
      setImportFile(data);
      setImportFileName(file.name);

      // Build detailed preview
      const hasGsKeys = servers.some(s => s.encrypted_gsocket_secret);
      setImportPreview({
        count: servers.length, hasKeys: hasKeys || hasGsKeys,
        servers: servers.map(s => {
          const mode = s.connection_mode || 'direct';
          return {
            name: s.name, mode,
            ip: mode === 'gsocket' ? 'GSSocket' : (s.ip_address || '-'),
            username: mode === 'gsocket' ? '-' : (s.ssh_username || '-'),
            port: s.ssh_port || 22,
            hasKey: !!(s.encrypted_key || s.encrypted_gsocket_secret),
            maskedKey: s.encrypted_key ? maskKey(s.encrypted_key) : s.encrypted_gsocket_secret ? maskKey(s.encrypted_gsocket_secret) : null,
            gsocketSecret: mode === 'gsocket' ? (s.gsocket_secret ? maskKey(s.gsocket_secret) : null) : null,
          };
        }),
      });
      setImportWithKeys(hasKeys || hasGsKeys);
    } catch (err) { setImportError('Failed to read file: ' + (err.message || 'Unknown error')); setImportFile(null); setImportFileName(''); }
  };

  const handleImport = async () => {
    if (!importFile) return; setImportError(''); setMsg(''); setImporting(true);
    try {
      const servers = importFile.servers || importFile;
      if (importWithKeys && !importPassword) { setImportError('Export password required to decrypt keys.'); setImporting(false); return; }
      const res = await api.importConfig(servers, importWithKeys, importPassword);
      setMsg(res.message + (res.note ? ' ' + res.note : '')); if (onRefresh) onRefresh();
      setImportFile(null); setImportFileName(''); setImportPreview(null); setImportPassword('');
    } catch (err) { setImportError(err.message || 'Import failed.'); setImporting(false); return; }
    setImporting(false);
  };

  const downloadSample = (withKeys) => {
    const sample = { version: "1.2.0", servers: [
      { name: "web-server", ip_address: "192.168.1.100", ssh_port: 22, ssh_username: "root", connection_mode: "direct", location: "Singapore", tags: ["production", "web"],
        ...(withKeys ? { encrypted_key: "(encrypted with AES-256-GCM using your export password — from Export with Keys)", public_key: "ssh-ed25519 AAAA...example..." } : {}) },
      { name: "nat-server", connection_mode: "gsocket", location: "Behind NAT", tags: ["internal"],
        ...(withKeys ? { encrypted_gsocket_secret: "(encrypted gsocket secret — from Export with Keys)" } : { gsocket_secret: "your-gsocket-secret-here" }) },
    ]};
    const blob = new Blob([JSON.stringify(sample, null, 2)], { type: 'application/json' });
    const a = document.createElement('a'); a.href = URL.createObjectURL(blob);
    a.download = `syncent-sample${withKeys ? '-with-keys' : ''}.json`; a.click(); URL.revokeObjectURL(a.href);
  };

  const inp = "w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none focus:border-teal-500";
  return (
    <div className="space-y-4">
      {msg&&<div className="bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 text-sm rounded-lg p-3">{msg}</div>}
      {error&&<div className="bg-red-500/10 border border-red-500/20 text-red-400 text-sm rounded-lg p-3">{error}</div>}

      {/* Export */}
      <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 space-y-4">
        <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Export Configuration</h3>
        <Toggle label="Include SSH Keys" desc="Encrypted with export password. Requires TOTP." value={exportWithKeys} onToggle={() => setExportWithKeys(!exportWithKeys)}/>
        {exportWithKeys && (
          <div className="space-y-3 pl-4 border-l-2 border-teal-500/30">
            <div><label className="text-xs text-zinc-500 block mb-1">Export Password</label>
              <input type="password" className={inp} value={exportPassword} onChange={e => setExportPassword(e.target.value)} placeholder="Strong password to protect keys"/></div>
            <div><label className="text-xs text-zinc-500 block mb-1">TOTP Code</label>
              <input className={`${inp} text-center font-mono tracking-widest w-40`} value={exportTotp} onChange={e => setExportTotp(e.target.value.replace(/\D/g,'').slice(0,6))} maxLength={6} placeholder="000000"/></div>
          </div>
        )}
        <button onClick={handleExport} disabled={exporting} className="px-4 py-2 bg-zinc-800 hover:bg-zinc-700 text-zinc-300 rounded-lg text-sm disabled:opacity-50">
          {exporting ? 'Exporting...' : `Export${exportWithKeys ? ' with Keys' : ''}`}
        </button>
      </div>

      {/* Import */}
      <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 space-y-4">
        <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Import Configuration</h3>
        <p className="text-xs text-zinc-500">Upload a SynCent backup (.json).</p>

        <div className="flex gap-2 items-center flex-wrap">
          <input ref={fileRef} type="file" accept=".json" onChange={handleFileSelect} className="hidden"/>
          <button onClick={() => fileRef.current?.click()}
            className="px-4 py-2.5 bg-zinc-800 hover:bg-zinc-700 text-zinc-300 rounded-lg text-sm border border-zinc-700 hover:border-zinc-600 transition-all">
            {importFileName || 'Choose File'}
          </button>
          <button onClick={() => downloadSample(false)} className="px-3 py-2.5 bg-zinc-800 hover:bg-zinc-700 text-zinc-400 rounded-lg text-xs border border-zinc-700">Sample JSON</button>
          <button onClick={() => downloadSample(true)} className="px-3 py-2.5 bg-zinc-800 hover:bg-zinc-700 text-zinc-400 rounded-lg text-xs border border-zinc-700">Sample with Keys</button>
        </div>

        {importError && <div className="bg-red-500/10 border border-red-500/20 text-red-400 text-xs rounded-lg p-2.5 whitespace-pre-wrap">{importError}</div>}

        {/* Preview */}
        {importPreview && (
          <div className="bg-zinc-800 rounded-lg p-4 space-y-3">
            <div className="flex items-center justify-between">
              <p className="text-xs text-zinc-300 font-medium">Preview: {importPreview.count} server{importPreview.count > 1 ? 's' : ''}</p>
              {importPreview.hasKeys && <span className="text-xs px-2 py-0.5 rounded-full bg-teal-500/20 text-teal-400">With Keys</span>}
            </div>

            <div className="divide-y divide-zinc-700/50 max-h-52 overflow-y-auto">
              {importPreview.servers.map((s, i) => (
                <div key={i} className="py-2 text-xs space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="text-zinc-200 font-medium">{s.name}</span>
                    <span className={`px-1.5 py-0.5 rounded text-[10px] ${s.mode === 'gsocket' ? 'bg-purple-500/20 text-purple-400' : 'bg-zinc-700 text-zinc-400'}`}>{s.mode}</span>
                  </div>
                  <div className="text-zinc-500 font-mono">
                    {s.mode === 'gsocket' ? 'GSSocket' : `${s.ip}:${s.port} (${s.username})`}
                  </div>
                  {s.hasKey ? (
                    <div className="text-zinc-600 font-mono text-[10px]">{s.mode === 'gsocket' ? 'Secret' : 'Key'}: {s.maskedKey}</div>
                  ) : s.gsocketSecret ? (
                    <div className="text-zinc-600 font-mono text-[10px]">Secret: {s.gsocketSecret}</div>
                  ) : s.mode === 'gsocket' ? (
                    <div className="text-zinc-700 text-[10px]">No gsocket secret</div>
                  ) : (
                    <div className="text-zinc-700 text-[10px]">No key (will be generated)</div>
                  )}
                </div>
              ))}
            </div>

            {importPreview.hasKeys && (
              <div className="mt-2"><label className="text-xs text-zinc-500 block mb-1">Export Password (to decrypt keys)</label>
                <input type="password" className={`${inp} w-64`} value={importPassword} onChange={e => setImportPassword(e.target.value)} placeholder="Password used during export"/></div>
            )}
          </div>
        )}

        <button onClick={handleImport} disabled={!importFile || importing}
          className="px-4 py-2 bg-teal-500 hover:bg-teal-400 disabled:opacity-50 text-zinc-950 rounded-lg text-sm font-semibold">
          {importing ? 'Importing...' : 'Import'}
        </button>
      </div>
    </div>
  );
}
