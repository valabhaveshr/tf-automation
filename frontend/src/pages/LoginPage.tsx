import { FormEvent, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../state/auth';
import { ApiError, ssoLoginUrl } from '../api/client';

export default function LoginPage() {
  const { login, loading, user } = useAuth();
  const navigate = useNavigate();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (user) {
      navigate('/records');
    }
  }, [user, navigate]);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await login(username, password);
      navigate('/records');
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError('Invalid username or password.');
      } else if (err instanceof ApiError && err.status === 429) {
        setError('Too many failed attempts. Try again later.');
      } else {
        setError('Login failed.');
      }
    }
  }

  return (
    <div>
      <h2>Log in</h2>

      {/*
        SSO is a browser redirect flow (GET /auth/sso/login -> 302 to the IdP
        -> GET /auth/sso/callback sets the session cookie -> 302 back here),
        not an XHR/fetch, so this is a plain link/full navigation rather than
        an onClick handler that calls the api client.
      */}
      <div className="field-row">
        <button type="button" onClick={() => window.location.assign(ssoLoginUrl('/records'))}>
          Log in with SSO
        </button>
      </div>

      <p className="field-help">
        Or log in with a username and password (retained during the SSO migration window):
      </p>

      <form onSubmit={handleSubmit}>
        <div className="field-row">
          <label>Username</label>
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />
        </div>
        <div className="field-row">
          <label>Password</label>
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        {error && <div className="field-error">{error}</div>}
        <button type="submit" disabled={loading}>
          {loading ? 'Logging in...' : 'Log in'}
        </button>
      </form>
    </div>
  );
}
