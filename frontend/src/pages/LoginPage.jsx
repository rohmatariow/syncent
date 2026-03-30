import React, { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import * as api from '../api/client';

// Minimal QR Code generator (no external dependency)
function QRCode({ value, size = 200 }) {
  const canvasRef = useRef(null);

  useEffect(() => {
    if (!value || !canvasRef.current) return;
    // Load qrcode lib from CDN
    const script = document.createElement('script');
    script.src = 'https://cdnjs.cloudflare.com/ajax/libs/qrcodejs/1.0.0/qrcode.min.js';
    script.onload = () => {
      const container = canvasRef.current;
      container.innerHTML = '';
      new window.QRCode(container, {
        text: value, width: size, height: size,
        colorDark: '#2dd4bf', colorLight: '#18181b',
        correctLevel: window.QRCode.CorrectLevel.M,
      });
    };
    document.head.appendChild(script);
    return () => { try { document.head.removeChild(script); } catch {} };
  }, [value, size]);

  return <div ref={canvasRef} className="inline-block rounded-lg overflow-hidden" />;
}

export default function LoginPage() {
  const navigate = useNavigate();
  const [mode, setMode] = useState('loading');
  const [step, setStep] = useState(1);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [totp, setTotp] = useState('');
  const [totpSecret, setTotpSecret] = useState('');
  const [totpUri, setTotpUri] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [captcha, setCaptcha] = useState(null);
  const [captchaAnswer, setCaptchaAnswer] = useState('');
  const [regAllowed, setRegAllowed] = useState(false);
  const [showManual, setShowManual] = useState(false);

  useEffect(() => {
    if (api.getToken()) { navigate('/'); return; }
    Promise.all([
      api.checkSetup(),
      api.isRegistrationAllowed().catch(() => ({ allowed: false })),
    ]).then(([setup, reg]) => {
      setRegAllowed(reg.allowed);
      setMode(setup.setup_required ? 'setup' : 'login');
    }).catch(() => setMode('login'));
  }, []);

  const loadCaptcha = async () => {
    try { setCaptcha(await api.getCaptcha()); setCaptchaAnswer(''); } catch {}
  };

  useEffect(() => { if (mode === 'login' && step === 1) loadCaptcha(); }, [mode]);

  const handleSetup = async (e) => {
    e.preventDefault(); setError(''); setLoading(true);
    try {
      const res = await api.setup(username, password);
      setTotpSecret(res.totp_secret || '');
      setTotpUri(res.totp_uri || '');
      setMode('verify');
    } catch (err) { setError(err.message || 'Setup failed.'); }
    setLoading(false);
  };

  const handleVerify = async (e) => {
    e.preventDefault(); setError(''); setLoading(true);
    try {
      await api.verifyTOTP(username, totp);
      setMode('login'); setStep(1); setTotp(''); setTotpSecret(''); setTotpUri(''); loadCaptcha();
    } catch (err) { setError(err.message || 'Invalid code.'); }
    setLoading(false);
  };

  const handleRegister = async (e) => {
    e.preventDefault(); setError(''); setLoading(true);
    try {
      const res = await api.register(username, password);
      setTotpSecret(res.totp_secret || '');
      setTotpUri(res.totp_uri || '');
      setMode('register-verify');
    } catch (err) { setError(err.message || 'Registration failed.'); }
    setLoading(false);
  };

  const handleUsernameSubmit = (e) => {
    e.preventDefault();
    if (!username.trim()) return;
    setError(''); setStep(2);
  };

  const handleCredentialsSubmit = async (e) => {
    e.preventDefault(); setError(''); setLoading(true);
    try {
      await api.validateCredentials(username, password);
      setStep(3);
    } catch (err) { setError(err.message || 'Invalid username or password.'); }
    setLoading(false);
  };

  const handleLogin = async (e) => {
    e.preventDefault(); setError(''); setLoading(true);
    try {
      const res = await api.login(username, password, totp);
      api.setToken(res.access_token);
      navigate('/');
    } catch (err) { setError(err.message || 'Invalid TOTP code.'); }
    setLoading(false);
  };

  if (mode === 'loading') return <div className="min-h-screen bg-zinc-950 flex items-center justify-center text-zinc-500">Loading...</div>;

  const inp = "w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2.5 text-sm text-zinc-200 outline-none focus:border-teal-500 transition-colors";
  const inpLocked = "w-full bg-zinc-800/50 border border-zinc-700/50 rounded-lg px-3 py-2.5 text-sm text-zinc-500 cursor-not-allowed";
  const btn = "w-full py-2.5 bg-teal-500 hover:bg-teal-400 disabled:opacity-50 text-zinc-950 font-semibold rounded-lg text-sm transition-colors";

  const subtitle = {
    setup: 'Create your admin account',
    verify: 'Set up two-factor authentication',
    'register-verify': 'Set up two-factor authentication',
    login: step === 1 ? 'Sign in to your dashboard' : step === 2 ? 'Enter your password' : 'Enter authenticator code',
    register: 'Create a new account',
  };

  return (
    <div className="min-h-screen bg-zinc-950 flex items-center justify-center p-4">
      <div className="w-full max-w-md">
        <div className="text-center mb-8">
          <div className="w-12 h-12 rounded-xl bg-gradient-to-br from-teal-400 to-cyan-500 flex items-center justify-center text-zinc-950 font-bold text-xl mx-auto mb-4">V</div>
          <h1 className="text-2xl font-bold text-zinc-100">SynCent</h1>
          <p className="text-zinc-500 text-sm mt-1">{subtitle[mode]}</p>
        </div>

        <div className="bg-zinc-900 border border-zinc-800 rounded-2xl p-6">
          {error && <div className="bg-red-500/10 border border-red-500/20 text-red-400 text-sm rounded-lg p-3 mb-4">{error}</div>}

          {/* SETUP */}
          {mode === 'setup' && (
            <form onSubmit={handleSetup} className="space-y-4">
              <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Username</label>
                <input className={inp} value={username} onChange={e => setUsername(e.target.value)} required /></div>
              <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Password (min 12 chars)</label>
                <input type="password" className={inp} value={password} onChange={e => setPassword(e.target.value)} minLength={12} required /></div>
              <button disabled={loading} className={btn}>{loading ? 'Creating...' : 'Create Admin Account'}</button>
            </form>
          )}

          {/* VERIFY TOTP (setup + register) */}
          {(mode === 'verify' || mode === 'register-verify') && (
            <form onSubmit={handleVerify} className="space-y-4">
              {/* QR Code */}
              {totpUri && (
                <div className="text-center">
                  <p className="text-xs text-zinc-400 mb-3">Scan this QR code with your authenticator app</p>
                  <div className="flex justify-center mb-3">
                    <QRCode value={totpUri} size={180} />
                  </div>
                  <button type="button" onClick={() => setShowManual(!showManual)}
                    className="text-xs text-zinc-600 hover:text-zinc-400 transition-colors">
                    {showManual ? 'Hide manual key' : "Can't scan? Enter key manually"}
                  </button>
                </div>
              )}

              {/* Manual secret (hidden by default) */}
              {(showManual || !totpUri) && totpSecret && (
                <div className="bg-zinc-800 rounded-lg p-4 text-center">
                  <p className="text-xs text-zinc-400 mb-2">Manual entry key:</p>
                  <code className="text-teal-400 text-sm font-mono break-all select-all block tracking-wider">{totpSecret}</code>
                </div>
              )}

              <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Verification Code</label>
                <input className={`${inp} text-center tracking-[0.5em] font-mono text-lg`} value={totp} onChange={e => setTotp(e.target.value.replace(/\D/g, '').slice(0, 6))} maxLength={6} placeholder="000000" required /></div>
              <button disabled={loading || totp.length !== 6} className={btn}>{loading ? 'Verifying...' : 'Verify & Enable 2FA'}</button>
            </form>
          )}

          {/* REGISTER */}
          {mode === 'register' && (
            <form onSubmit={handleRegister} className="space-y-4">
              <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Username</label>
                <input className={inp} value={username} onChange={e => setUsername(e.target.value)} minLength={3} maxLength={50} pattern="[a-zA-Z0-9_]+" title="Letters, numbers, underscore only" required /></div>
              <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Password (min 12 chars)</label>
                <input type="password" className={inp} value={password} onChange={e => setPassword(e.target.value)} minLength={12} required /></div>
              <button disabled={loading} className={btn}>{loading ? 'Creating...' : 'Create Account'}</button>
              <button type="button" onClick={() => { setMode('login'); setStep(1); setError(''); }} className="w-full text-xs text-zinc-600 hover:text-zinc-400 mt-2">← Back to login</button>
            </form>
          )}

          {/* LOGIN STEP 1 */}
          {mode === 'login' && step === 1 && (
            <form onSubmit={handleUsernameSubmit} className="space-y-4">
              <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Username</label>
                <input className={inp} value={username} onChange={e => setUsername(e.target.value)} autoFocus required /></div>
              {captcha && (
                <div>
                  <label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">{captcha.question}</label>
                  <div className="flex gap-2">
                    <input className={`${inp} flex-1 text-center font-mono`} value={captchaAnswer} onChange={e => setCaptchaAnswer(e.target.value)} placeholder="Answer" required />
                    <button type="button" onClick={loadCaptcha} className="px-3 py-2 bg-zinc-800 hover:bg-zinc-700 text-zinc-400 rounded-lg text-sm shrink-0">↻</button>
                  </div>
                </div>
              )}
              <button disabled={!username.trim()} className={btn}>Continue</button>
              {regAllowed && (
                <button type="button" onClick={() => { setMode('register'); setError(''); }} className="w-full text-xs text-teal-500 hover:text-teal-400 mt-2">Don't have an account? Register</button>
              )}
            </form>
          )}

          {/* LOGIN STEP 2 */}
          {mode === 'login' && step === 2 && (
            <form onSubmit={handleCredentialsSubmit} className="space-y-4">
              <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Username</label>
                <input className={inpLocked} value={username} disabled /></div>
              <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Password</label>
                <input type="password" className={inp} value={password} onChange={e => setPassword(e.target.value)} autoFocus required /></div>
              <button disabled={loading || !password} className={btn}>{loading ? 'Checking...' : 'Continue'}</button>
              <button type="button" onClick={() => { setStep(1); setPassword(''); setError(''); }} className="w-full text-xs text-zinc-600 hover:text-zinc-400">← Change username</button>
            </form>
          )}

          {/* LOGIN STEP 3 */}
          {mode === 'login' && step === 3 && (
            <form onSubmit={handleLogin} className="space-y-4">
              <div className="bg-zinc-800/50 rounded-lg p-3 text-center">
                <span className="text-xs text-zinc-500">Signing in as </span>
                <span className="text-xs text-teal-400 font-mono">{username}</span>
              </div>
              <div><label className="text-xs text-zinc-500 uppercase tracking-wider block mb-1.5">Authenticator Code</label>
                <input className={`${inp} text-center tracking-[0.5em] font-mono text-lg`} value={totp} onChange={e => setTotp(e.target.value.replace(/\D/g, '').slice(0, 6))} maxLength={6} placeholder="000000" autoFocus required /></div>
              <button disabled={loading || totp.length !== 6} className={btn}>{loading ? 'Signing in...' : 'Sign In'}</button>
              <button type="button" onClick={() => { setStep(2); setTotp(''); setError(''); }} className="w-full text-xs text-zinc-600 hover:text-zinc-400">← Back</button>
            </form>
          )}
        </div>
      </div>
    </div>
  );
}
