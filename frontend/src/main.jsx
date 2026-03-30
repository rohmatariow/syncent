import React, { useState, useEffect } from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import './index.css';
import { getToken, setToken, onAuthFailure } from './api/client';
import LoginPage from './pages/LoginPage';
import DashboardPage from './pages/DashboardPage';

function ProtectedRoute({ children }) {
  if (!getToken()) return <Navigate to="/login" />;
  return children;
}

function App() {
  const [loading, setLoading] = useState(true);
  const [, setForce] = useState(0);

  useEffect(() => {
    onAuthFailure(() => { setForce(n => n + 1); window.location.href = '/login'; });
    if (!getToken()) {
      fetch('/api/auth/refresh', { method: 'POST', credentials: 'include' })
        .then(r => r.ok ? r.json() : null)
        .then(d => { if (d?.access_token) setToken(d.access_token); })
        .catch(() => {})
        .finally(() => setLoading(false));
    } else {
      setLoading(false);
    }
  }, []);

  if (loading) return <div className="min-h-screen bg-zinc-950 flex items-center justify-center"><div className="text-zinc-500 text-sm">Loading...</div></div>;

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/*" element={<ProtectedRoute><DashboardPage /></ProtectedRoute>} />
      </Routes>
    </BrowserRouter>
  );
}

ReactDOM.createRoot(document.getElementById('root')).render(<App />);
