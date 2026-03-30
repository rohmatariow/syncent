const BASE = '';

let accessToken = '';
let onAuthFail = () => {};

export function setToken(t) { accessToken = t; }
export function getToken() { return accessToken; }
export function clearToken() { accessToken = ''; }
export function onAuthFailure(fn) { onAuthFail = fn; }

async function request(method, path, body, skipAuth) {
  const headers = { 'Content-Type': 'application/json' };
  if (!skipAuth && accessToken) headers['Authorization'] = `Bearer ${accessToken}`;
  const res = await fetch(BASE + path, { method, headers, credentials: 'include', body: body ? JSON.stringify(body) : undefined });
  if (res.status === 401 && !skipAuth) {
    const rr = await fetch(BASE + '/api/auth/refresh', { method: 'POST', credentials: 'include' });
    if (rr.ok) { const d = await rr.json(); setToken(d.access_token); headers['Authorization'] = `Bearer ${d.access_token}`; return (await fetch(BASE + path, { method, headers, credentials: 'include', body: body ? JSON.stringify(body) : undefined })).json(); }
    clearToken(); onAuthFail(); throw new Error('Session expired');
  }
  const data = await res.json();
  if (!res.ok) throw { status: res.status, ...data };
  return data;
}

// Auth
export const checkSetup = () => request('GET', '/api/auth/check-setup', null, true);
export const setup = (u, p) => request('POST', '/api/auth/setup', { username: u, password: p }, true);
export const verifyTOTP = (u, c) => request('POST', '/api/auth/setup/verify', { username: u, totp_code: c }, true);
export const login = (u, p, c) => request('POST', '/api/auth/login', { username: u, password: p, totp_code: c }, true);
export const logout = () => request('POST', '/api/auth/logout');
export const me = () => request('GET', '/api/auth/me');
export const getCaptcha = () => request('GET', '/api/auth/captcha', null, true);
export const validateCredentials = (u, p) => request('POST', '/api/auth/validate-credentials', { username: u, password: p }, true);
export const get2FAStatus = () => request('GET', '/api/auth/2fa-status');
export const toggle2FA = (enable, totp_code, confirmation) => request('POST', '/api/admin/toggle-2fa', { enable, totp_code, confirmation });
export const isRegistrationAllowed = () => request('GET', '/api/auth/registration-allowed', null, true);
export const register = (u, p) => request('POST', '/api/auth/register', { username: u, password: p }, true);

// Servers
export const listServers = () => request('GET', '/api/servers/');
export const getServer = id => request('GET', `/api/servers/${id}`);
export const createServer = d => request('POST', '/api/servers/', d);
export const updateServer = (id, d) => request('PUT', `/api/servers/${id}`, d);
export const deleteServer = (id, totp_code, confirmation) => request('DELETE', `/api/servers/${id}`, { totp_code, confirmation });
export const injectKey = (id, pw) => request('POST', `/api/servers/${id}/inject-key`, { ssh_password: pw });
export const manualKey = id => request('POST', `/api/servers/${id}/manual-key`, { confirmed: true });
export const testConnection = id => request('POST', `/api/servers/${id}/test-connection`);
export const getPublicKey = id => request('GET', `/api/servers/${id}/public-key`);
export const getReinjectKey = id => request('GET', `/api/servers/${id}/reinject-key`);

// Metrics
export const getCurrentMetrics = id => request('GET', `/api/servers/${id}/metrics/current`);
export const getMetricsHistory = (id, r) => request('GET', `/api/servers/${id}/metrics/history?range=${r}`);
export const collectMetrics = id => request('POST', `/api/servers/${id}/metrics/collect`);
export const getAvailability = id => request('GET', `/api/servers/${id}/availability`);

// Commands
export const executeCommand = (id, cmd, timeout, totp, confirmation) => request('POST', `/api/servers/${id}/execute`, { command: cmd, timeout, totp_code: totp, confirmation });
export const classifyCommand = (id, cmd) => request('GET', `/api/servers/${id}/execute/classify?command=${encodeURIComponent(cmd)}`);
export const listSnippets = () => request('GET', '/api/servers/snippets');
export const createSnippet = (n, c, cat) => request('POST', '/api/servers/snippets', { name: n, command: c, category: cat });
export const deleteSnippet = id => request('DELETE', `/api/servers/snippets/${id}`);

// History
export const getServerHistory = (id, p, l) => request('GET', `/api/servers/${id}/history?page=${p||1}&limit=${l||50}`);
export const getStatusHistory = (id, r) => request('GET', `/api/servers/${id}/status-history?range=${r||'7d'}`);

// Admin
export const getDashboard = () => request('GET', '/api/admin/dashboard');
export const getAuditLog = (p, l) => request('GET', `/api/admin/audit?page=${p||1}&limit=${l||50}`);
export const verifyAuditChain = () => request('GET', '/api/admin/audit/verify');
export const exportConfig = (withKeys, totpCode, exportPw) => request('POST', '/api/admin/config/export', { with_keys: withKeys, totp_code: totpCode, export_password: exportPw });
export const importConfig = (servers, withKeys, exportPw) => request('POST', '/api/admin/config/import', { servers, with_keys: withKeys, export_password: exportPw });
export const resetPassword = (cur, nw, totp) => request('POST', '/api/admin/reset-password', { current_password: cur, new_password: nw, totp_code: totp });
export const getAppConfig = () => request('GET', '/api/admin/app-config');
export const updateAppConfig = c => request('PUT', '/api/admin/app-config', c);

export function wsUrl(path) {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${location.host}${path}?token=${accessToken}`;
}
