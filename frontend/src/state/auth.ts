/**
 * Current-session store. Session identity itself lives server-side in the
 * tf_automation_session cookie (see api/client.ts); this context caches
 * GET /auth/me — the user, their customer, and the routing flags
 * (needs_onboarding, platform/customer admin) — so components don't refetch
 * it per render.
 *
 * `refresh()` re-reads /auth/me after anything that changes it: login,
 * accepting an invitation, completing onboarding, editing the organization.
 */
import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { Me } from '../api/types';
import * as api from '../api/client';

export interface AuthApi {
  me: Me | null;
  /** True until the first /auth/me check finishes, so guards don't bounce to /login on a valid session. */
  initializing: boolean;
  login: (username: string, password: string) => Promise<Me | null>;
  logout: () => Promise<void>;
  refresh: () => Promise<Me | null>;
}

const AuthContext = createContext<AuthApi | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [me, setMe] = useState<Me | null>(null);
  const [initializing, setInitializing] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const res = await api.getCurrentUser();
      setMe(res);
      return res;
    } catch {
      // 401 = no valid session; anything else also leaves us signed out.
      setMe(null);
      return null;
    }
  }, []);

  useEffect(() => {
    refresh().finally(() => setInitializing(false));
  }, [refresh]);

  const login = useCallback(
    async (username: string, password: string) => {
      await api.login({ username, password });
      return refresh();
    },
    [refresh],
  );

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setMe(null);
    }
  }, []);

  const value = useMemo<AuthApi>(
    () => ({ me, initializing, login, logout, refresh }),
    [me, initializing, login, logout, refresh],
  );

  return React.createElement(AuthContext.Provider, { value }, children);
}

export function useAuth(): AuthApi {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return ctx;
}
