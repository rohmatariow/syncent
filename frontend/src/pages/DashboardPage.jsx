import React, { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import * as api from '../api/client';
import AddServerModal from '../components/AddServerModal';
import MetricsPanel from '../components/MetricsPanel';
import LogViewer from '../components/LogViewer';
import TerminalView from '../components/TerminalView';
import CommandPanel from '../components/CommandPanel';
import HistoryPanel from '../components/HistoryPanel';
import HomeDashboard from '../components/HomeDashboard';
import AppSettings from '../components/AppSettings';

const SC = { online:'bg-emerald-400', offline:'bg-red-400', unreachable:'bg-red-400', ssh_down:'bg-orange-400', auth_failed:'bg-amber-400', pending_setup:'bg-zinc-500', unknown:'bg-zinc-500' };
function StatusDot({s}) { return <span className="relative flex items-center"><span className={`w-2.5 h-2.5 rounded-full ${SC[s]||'bg-zinc-500'}`}/>{s==='online'&&<span className="absolute w-2.5 h-2.5 rounded-full bg-emerald-400 animate-ping opacity-40"/>}</span>; }

const TABS = ['overview','logs','terminal','commands','history','settings'];

export default function DashboardPage() {
  const navigate = useNavigate();
  const [servers, setServers] = useState([]);
  const [selected, setSelected] = useState(null);
  const [tab, setTab] = useState('overview');
  const [view, setView] = useState('home');
  const [showAddModal, setShowAddModal] = useState(false);
  const [addMode, setAddMode] = useState('direct');
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [serversExpanded, setServersExpanded] = useState(true);
  const [sshExpanded, setSshExpanded] = useState(true);
  const [gsExpanded, setGsExpanded] = useState(true);
  const [deleteTarget, setDeleteTarget] = useState(null);
  const [deleteTotp, setDeleteTotp] = useState('');
  const [deleteConfirm, setDeleteConfirm] = useState('');
  const [deleteError, setDeleteError] = useState('');
  const [tfaRequired, setTfaRequired] = useState(true);

  const loadServers = useCallback(async () => {
    try {
      const list = await api.listServers();
      setServers(list);
      if (selected) {
        const u = list.find(s => s.id === selected.id);
        if (u) setSelected(u); else { setSelected(null); setView('home'); }
      }
    } catch {}
  }, [selected]);

  useEffect(() => {
    loadServers();
    api.get2FAStatus().then(d => setTfaRequired(d.required)).catch(() => {});
    const i = setInterval(loadServers, 15000); return () => clearInterval(i);
  }, []);

  const handleLogout = async () => { try { await api.logout(); } catch {} api.clearToken(); navigate('/login'); };
  const selectServer = (s) => { setSelected(s); setView('server'); setTab('overview'); };
  const handleDeleteComplete = () => { setSelected(null); setView('home'); loadServers(); };

  const handleQuickDelete = async () => {
    if (!deleteTarget) return;
    if (tfaRequired && deleteTotp.length !== 6) return;
    if (!tfaRequired && deleteConfirm !== 'DELETE') return;
    setDeleteError('');
    try {
      await api.deleteServer(deleteTarget.id, tfaRequired ? deleteTotp : '', tfaRequired ? '' : deleteConfirm);
      setDeleteTarget(null); setDeleteTotp('');
      if (selected?.id === deleteTarget.id) { setSelected(null); setView('home'); }
      loadServers();
    } catch (err) { setDeleteError(err.message || 'Delete failed.'); }
  };

  const sshServers = servers.filter(s => s.connection_mode !== 'gsocket');
  const gsServers = servers.filter(s => s.connection_mode === 'gsocket');

  function ServerItem({ server }) {
    return (
      <div className={`group flex items-center transition-all ${selected?.id===server.id&&view==='server'?'bg-zinc-800/80 border-r-2 border-teal-400':''}`}>
        <button onClick={() => selectServer(server)} className="flex-1 flex items-center gap-2.5 pl-6 pr-2 py-2 text-left hover:bg-zinc-800/40 min-w-0">
          <StatusDot s={server.status}/>
          <div className="min-w-0 flex-1">
            <div className="text-xs font-medium text-zinc-200 truncate">{server.name}</div>
            <div className="text-[10px] text-zinc-600 font-mono truncate">{server.connection_mode === 'gsocket' ? 'gsocket' : server.ip_address}</div>
          </div>
        </button>
        <button onClick={(e) => { e.stopPropagation(); setDeleteTarget(server); setDeleteTotp(''); setDeleteConfirm(''); setDeleteError(''); }}
          className="opacity-0 group-hover:opacity-100 px-2 py-1 text-zinc-600 hover:text-red-400 text-xs transition-all shrink-0" title="Delete">✕</button>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-zinc-950 text-zinc-100 flex">
      <aside className={`${sidebarOpen?'w-72':'w-16'} bg-zinc-900/80 border-r border-zinc-800 flex flex-col transition-all duration-300 shrink-0`}>
        <div className="p-4 border-b border-zinc-800 flex items-center gap-3">
          {sidebarOpen&&<div className="flex items-center gap-2.5 flex-1 min-w-0">
            <button onClick={() => { setView('home'); setSelected(null); }} className="w-8 h-8 rounded-lg bg-gradient-to-br from-teal-400 to-cyan-500 flex items-center justify-center text-zinc-950 font-bold text-sm shrink-0 hover:opacity-80 transition-opacity">V</button>
            <div className="min-w-0"><h1 className="text-sm font-bold tracking-tight truncate cursor-pointer" onClick={() => { setView('home'); setSelected(null); }}>SynCent</h1></div>
          </div>}
          <button onClick={() => setSidebarOpen(!sidebarOpen)} className="text-zinc-500 hover:text-zinc-300 text-lg shrink-0">{sidebarOpen?'◀':'▶'}</button>
        </div>

        {sidebarOpen ? (
          <div className="flex-1 overflow-y-auto py-2">
            <button onClick={() => { setView('home'); setSelected(null); }}
              className={`w-full flex items-center px-4 py-2.5 text-left text-sm transition-all ${view==='home'?'bg-zinc-800/80 border-r-2 border-teal-400 text-zinc-100':'text-zinc-400 hover:bg-zinc-800/40 hover:text-zinc-200'}`}>
              Dashboard
            </button>

            {/* Servers group */}
            <button onClick={() => setServersExpanded(!serversExpanded)}
              className="w-full flex items-center px-4 py-2.5 text-left text-sm text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/40 transition-all mt-1">
              <span className="flex-1">Servers</span>
              <span className="text-xs text-zinc-600 mr-2">{servers.length}</span>
              <span className="text-zinc-600 text-xs">{serversExpanded ? '▾' : '▸'}</span>
            </button>

            {serversExpanded && (
              <div className="border-l border-zinc-800/60 ml-3">
                {/* SSH submenu */}
                <button onClick={() => setSshExpanded(!sshExpanded)}
                  className="w-full flex items-center pl-4 pr-4 py-2 text-left text-xs text-zinc-500 hover:text-zinc-300 transition-all">
                  <span className="flex-1">SSH</span>
                  <span className="text-[10px] text-zinc-600 mr-2">{sshServers.length}</span>
                  <span className="text-zinc-700 text-[10px]">{sshExpanded ? '▾' : '▸'}</span>
                </button>
                {sshExpanded && sshServers.map(s => <ServerItem key={s.id} server={s} />)}
                {sshExpanded && <button onClick={() => { setShowAddModal(true); setAddMode('direct'); }}
                  className="w-full pl-6 pr-4 py-1.5 text-[10px] text-zinc-600 hover:text-teal-400 text-left transition-all">+ Add SSH Server</button>}

                {/* GSSocket submenu */}
                <button onClick={() => setGsExpanded(!gsExpanded)}
                  className="w-full flex items-center pl-4 pr-4 py-2 text-left text-xs text-zinc-500 hover:text-zinc-300 transition-all">
                  <span className="flex-1">GSSocket</span>
                  <span className="text-[10px] text-zinc-600 mr-2">{gsServers.length}</span>
                  <span className="text-zinc-700 text-[10px]">{gsExpanded ? '▾' : '▸'}</span>
                </button>
                {gsExpanded && gsServers.map(s => <ServerItem key={s.id} server={s} />)}
                {gsExpanded && <button onClick={() => { setShowAddModal(true); setAddMode('gsocket'); }}
                  className="w-full pl-6 pr-4 py-1.5 text-[10px] text-zinc-600 hover:text-teal-400 text-left transition-all">+ Add GSSocket Server</button>}
              </div>
            )}

            <button onClick={() => { setView('settings'); setSelected(null); }}
              className={`w-full flex items-center px-4 py-2.5 text-left text-sm transition-all mt-1 ${view==='settings'?'bg-zinc-800/80 border-r-2 border-teal-400 text-zinc-100':'text-zinc-400 hover:bg-zinc-800/40 hover:text-zinc-200'}`}>
              Settings
            </button>
          </div>
        ) : (
          <div className="flex-1 overflow-y-auto py-2">
            <button onClick={() => { setView('home'); setSelected(null); }} className={`w-full py-3 text-center text-xs ${view==='home'?'text-teal-400':'text-zinc-500'}`} title="Dashboard">D</button>
            {servers.map(s => (
              <button key={s.id} onClick={() => selectServer(s)} className={`w-full py-2 flex justify-center ${selected?.id===s.id?'border-r-2 border-teal-400':''}`}><StatusDot s={s.status}/></button>
            ))}
            <button onClick={() => { setView('settings'); setSelected(null); }} className={`w-full py-3 text-center text-xs ${view==='settings'?'text-teal-400':'text-zinc-500'}`} title="Settings">S</button>
          </div>
        )}

        {sidebarOpen && <div className="p-3 border-t border-zinc-800">
          <button onClick={handleLogout} className="w-full py-2 text-sm text-zinc-600 hover:text-red-400 transition-all">Logout</button>
        </div>}
      </aside>

      <main className="flex-1 flex flex-col min-w-0 overflow-hidden">
        {view === 'home' && <div className="flex-1 overflow-y-auto p-6"><HomeDashboard servers={servers} onSelectServer={selectServer}/></div>}
        {view === 'settings' && <div className="flex-1 overflow-y-auto p-6"><AppSettings onRefresh={loadServers}/></div>}
        {view === 'server' && selected && <>
          <header className="px-6 py-4 border-b border-zinc-800 bg-zinc-900/50">
            <div className="flex items-center gap-4">
              <StatusDot s={selected.status}/>
              <div className="flex-1">
                <h2 className="text-lg font-semibold">{selected.name}</h2>
                <div className="flex items-center gap-3 text-xs text-zinc-500 flex-wrap">
                  <span className="font-mono">{selected.connection_mode === 'gsocket' ? 'GSSocket' : `${selected.ip_address}:${selected.ssh_port||22}`}</span>
                  {selected.location&&<><span>·</span><span>{selected.location}</span></>}
                  <span>·</span><span className={SC[selected.status]?.replace('bg-','text-')||'text-zinc-500'}>{selected.status?.toUpperCase()}</span>
                  {selected.last_ping_ms!=null&&<><span>·</span><span>{selected.last_ping_ms.toFixed(0)}ms</span></>}
                </div>
              </div>
            </div>
            <div className="flex gap-1 mt-4 overflow-x-auto">
              {TABS.map(t => <button key={t} onClick={() => setTab(t)} className={`px-4 py-1.5 rounded-md text-sm font-medium transition-all capitalize shrink-0 ${tab===t?'bg-zinc-800 text-zinc-100':'text-zinc-500 hover:text-zinc-300'}`}>{t}</button>)}
            </div>
          </header>
          <div className="flex-1 overflow-y-auto p-6">
            {tab==='overview'&&<MetricsPanel server={selected} onRefresh={loadServers}/>}
            {tab==='logs'&&<LogViewer server={selected}/>}
            {tab==='terminal'&&<TerminalView server={selected}/>}
            {tab==='commands'&&<CommandPanel server={selected} tfaRequired={tfaRequired}/>}
            {tab==='history'&&<HistoryPanel server={selected}/>}
            {tab==='settings'&&<ServerSettings server={selected} onRefresh={loadServers} onDeleted={handleDeleteComplete} tfaRequired={tfaRequired}/>}
          </div>
        </>}
        {view === 'server' && !selected && <div className="flex-1 flex items-center justify-center text-zinc-600">Select a server from the sidebar.</div>}
      </main>

      {deleteTarget && (
        <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-50" onClick={() => setDeleteTarget(null)}>
          <div className="bg-zinc-900 border border-zinc-700 rounded-2xl w-full max-w-sm p-6 shadow-2xl" onClick={e => e.stopPropagation()}>
            <h3 className="text-sm font-semibold mb-1">Delete Server</h3>
            <p className="text-xs text-zinc-500 mb-4">Remove <strong className="text-zinc-200">{deleteTarget.name}</strong>?</p>
            {deleteError && <div className="bg-red-500/10 border border-red-500/20 text-red-400 text-xs rounded-lg p-2.5 mb-3">{deleteError}</div>}
            <div className="space-y-3">
              {tfaRequired ? (
                <div><label className="text-xs text-zinc-500 block mb-1">TOTP Code</label>
                  <input className="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none focus:border-teal-500 text-center font-mono tracking-widest"
                    value={deleteTotp} onChange={e => setDeleteTotp(e.target.value.replace(/\D/g,'').slice(0,6))} maxLength={6} placeholder="000000" autoFocus/></div>
              ) : (
                <div><label className="text-xs text-zinc-500 block mb-1">Type <code className="text-red-400">DELETE</code> to confirm</label>
                  <input className="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none focus:border-teal-500"
                    value={deleteConfirm} onChange={e => setDeleteConfirm(e.target.value)} placeholder="DELETE" autoFocus/></div>
              )}
              <div className="flex gap-2">
                <button onClick={handleQuickDelete} disabled={tfaRequired ? deleteTotp.length !== 6 : deleteConfirm !== 'DELETE'} className="flex-1 py-2 bg-red-500 hover:bg-red-600 disabled:opacity-50 text-white font-semibold rounded-lg text-sm">Delete</button>
                <button onClick={() => setDeleteTarget(null)} className="flex-1 py-2 bg-zinc-800 text-zinc-400 hover:bg-zinc-700 rounded-lg text-sm">Cancel</button>
              </div>
            </div>
          </div>
        </div>
      )}

      {showAddModal&&<AddServerModal defaultMode={addMode} onClose={() => setShowAddModal(false)} onAdded={() => { setShowAddModal(false); loadServers(); }}/>}
    </div>
  );
}

function ServerSettings({ server, onRefresh, onDeleted, tfaRequired = true }) {
  const [name, setName] = useState('');
  const [location, setLocation] = useState('');
  const [notes, setNotes] = useState('');
  const [tags, setTags] = useState('');
  const [saving, setSaving] = useState(false);
  const [deleteTotp, setDeleteTotp] = useState('');
  const [deleteConfirm, setDeleteConfirm] = useState('');
  const [showDelete, setShowDelete] = useState(false);
  const [error, setError] = useState('');
  const [reinjectInfo, setReinjectInfo] = useState(null);
  const [showKey, setShowKey] = useState(false);

  useEffect(() => {
    setName(server.name||''); setLocation(server.location||'');
    setNotes(server.notes||''); setTags(Array.isArray(server.tags) ? server.tags.join(', ') : '');
    setShowDelete(false); setError(''); setReinjectInfo(null); setShowKey(false);
  }, [server]);

  const handleSave = async () => {
    setSaving(true);
    try { await api.updateServer(server.id, { name, location, notes, tags: tags.split(',').map(t => t.trim()).filter(Boolean) }); onRefresh(); } catch {}
    setSaving(false);
  };
  const handleDelete = async () => { setError(''); try { await api.deleteServer(server.id, tfaRequired ? deleteTotp : '', tfaRequired ? '' : deleteConfirm); onDeleted(); } catch (err) { setError(err.message || 'Delete failed.'); } };
  const handleToggleKey = async () => { if (showKey) { setShowKey(false); setReinjectInfo(null); return; } try { const r = await api.getReinjectKey(server.id); setReinjectInfo(r); setShowKey(true); } catch {} };

  const inp = "w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-zinc-200 outline-none focus:border-teal-500";
  return (
    <div className="space-y-4 max-w-xl">
      <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 space-y-4">
        <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">Server Configuration</h3>
        <div><label className="text-xs text-zinc-500 block mb-1.5">Display Name</label><input className={inp} value={name} onChange={e => setName(e.target.value)}/></div>
        <div><label className="text-xs text-zinc-500 block mb-1.5">Location</label><input className={inp} value={location} onChange={e => setLocation(e.target.value)}/></div>
        <div><label className="text-xs text-zinc-500 block mb-1.5">Tags (comma separated)</label><input className={inp} value={tags} onChange={e => setTags(e.target.value)}/></div>
        <div><label className="text-xs text-zinc-500 block mb-1.5">Notes</label><textarea className={`${inp} h-20 resize-none`} value={notes} onChange={e => setNotes(e.target.value)}/></div>
        <button onClick={handleSave} disabled={saving} className="px-4 py-2 bg-teal-500 hover:bg-teal-400 disabled:opacity-50 text-zinc-950 font-semibold rounded-lg text-sm">{saving?'Saving...':'Save Changes'}</button>
      </div>

      {server.connection_mode !== 'gsocket' && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5 space-y-3">
          <h3 className="text-xs text-zinc-500 uppercase tracking-wider font-medium">SSH Key</h3>
          <button onClick={handleToggleKey} className="px-4 py-2 bg-zinc-800 hover:bg-zinc-700 text-zinc-300 rounded-lg text-sm">{showKey ? 'Hide Public Key' : 'Show Public Key'}</button>
          {showKey && reinjectInfo && (
            <div className="space-y-2 mt-2">
              <code className="text-xs text-teal-400 break-all select-all block bg-zinc-800 p-3 rounded-lg">{reinjectInfo.public_key}</code>
              <p className="text-xs text-zinc-600">Run on server: <code className="text-zinc-400">{reinjectInfo.manual_command}</code></p>
            </div>
          )}
        </div>
      )}

      <div className="bg-zinc-900 border border-red-900/30 rounded-xl p-5">
        <h3 className="text-xs text-red-400 uppercase tracking-wider font-medium mb-3">Danger Zone</h3>
        {error && <div className="bg-red-500/10 border border-red-500/20 text-red-400 text-sm rounded-lg p-3 mb-3">{error}</div>}
        {!showDelete ? <button onClick={() => setShowDelete(true)} className="px-4 py-2 bg-red-500/10 text-red-400 hover:bg-red-500/20 border border-red-500/20 rounded-lg text-sm">Remove Server</button> : (
          <div className="space-y-3">
            {tfaRequired ? (<>
              <p className="text-sm text-zinc-400">Enter TOTP code to confirm:</p>
              <input className={`${inp} text-center font-mono tracking-widest`} value={deleteTotp} onChange={e => setDeleteTotp(e.target.value.replace(/\D/g,'').slice(0,6))} maxLength={6} placeholder="000000"/>
            </>) : (<>
              <p className="text-sm text-zinc-400">Type <code className="text-red-400">DELETE</code> to confirm:</p>
              <input className={inp} value={deleteConfirm} onChange={e => setDeleteConfirm(e.target.value)} placeholder="DELETE"/>
            </>)}
            <div className="flex gap-2">
              <button onClick={handleDelete} disabled={tfaRequired ? deleteTotp.length!==6 : deleteConfirm!=='DELETE'} className="px-4 py-2 bg-red-500 text-white rounded-lg text-sm disabled:opacity-50">Confirm Delete</button>
              <button onClick={() => { setShowDelete(false); setError(''); }} className="px-4 py-2 bg-zinc-800 text-zinc-400 rounded-lg text-sm">Cancel</button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
