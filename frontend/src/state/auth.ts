/**
 * Minimal current-user store. Session identity itself lives server-side in
 * the tf_automation_session cookie (see api/client.ts); this context just
 * caches the logged-in User so components (e.g. CatalogBrowser's admin-only
 * refresh button) can read is_admin without an extra round trip per render.
 */
import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { User } from '../api/types';
import * as api from '../api/client';

export interface AuthApi {
  user: User | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<User>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthApi | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(false);

  // Closes the gap App.tsx previously flagged: GET /auth/sso/callback is a
  // full browser redirect (the SPA is torn down and reloaded), so any
  // in-memory `user` state from an explicit login call is lost by the time
  // the app lands back on e.g. /records. Without this, `user` stays null
  // forever after an SSO login and RequireAuth bounces to /login even
  // though the session cookie is valid. Calling GET /auth/me on mount
  // recovers identity from the cookie alone. A 401 here just means "not
  // logged in yet" (the common case on a fresh visit) -- not an error to
  // surface, `user` simply stays null and RequireAuth routes to /login as
  // before.
  //
  // Note: this fetch is async, so on the very first render after a
  // full-page load `user` is still null -- a route gated by App.tsx's
  // RequireAuth will render its /login redirect before this resolves.
  // LoginPage already re-navigates to /records once `user` becomes
  // non-null (see its own useEffect), so the net effect is a brief flash
  // through /login rather than a hard bounce; a loading-aware route guard
  // in App.tsx would remove that flash entirely but is out of this file's
  // scope.
  useEffect(() => {
    let cancelled = false;
    api
      .getCurrentUser()
      .then((res) => {
        if (!cancelled) {
          setUser(res.user);
        }
      })
      .catch(() => {
        // No valid session, or a transient network error -- leave user
        // null.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const login = useCallback(async (username: string, password: string) => {
    setLoading(true);
    try {
      const res = await api.login({ username, password });
      setUser(res.user);
      return res.user;
    } finally {
      setLoading(false);
    }
  }, []);

  const logout = useCallback(async () => {
    await api.logout();
    setUser(null);
  }, []);

  const value = useMemo<AuthApi>(() => ({ user, loading, login, logout }), [user, loading, login, logout]);

  return React.createElement(AuthContext.Provider, { value }, children);
}

export function useAuth(): AuthApi {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return ctx;
}
