import { Navigate, Route, Routes, Link } from 'react-router-dom';
import { useAuth } from './state/auth';
import { isAdmin } from './api/types';
import LoginPage from './pages/LoginPage';
import RecordListPage from './pages/RecordListPage';
import RecordEditorPage from './pages/RecordEditorPage';
import RecordDetailPage from './pages/RecordDetailPage';
import AuditLogPage from './pages/AuditLogPage';

/**
 * SSO callback landing note (see api/client.ts ssoLoginUrl and
 * pages/LoginPage.tsx): GET /auth/sso/callback is a full browser redirect
 * (302) back to this app after setting the session cookie, not an XHR — the
 * SPA is torn down and reloaded from scratch, so any in-memory `user` state
 * is lost. LoginPage requests redirect_to=/records, which RequireAuth below
 * gates on `user` being set.
 *
 * KNOWN GAP (flagged for follow-up, out of this change's file scope): there
 * is currently no bootstrap-on-mount fetch of the current user anywhere in
 * this app (state/auth.ts's AuthProvider only sets `user` as a side effect
 * of an explicit POST /auth/login call), and contracts/openapi.yaml has no
 * "who am I" endpoint (e.g. GET /auth/me) for AuthProvider to call on mount.
 * As-is, landing on /records post-SSO-redirect will bounce back to /login
 * even though the session cookie is valid, because `user` is null on the
 * fresh page load. Fixing this needs: (1) a GET /auth/me (or similar)
 * endpoint added to contracts/openapi.yaml as a coordinated contract change,
 * and (2) AuthProvider calling it in a useEffect on mount before rendering
 * routes. Both are outside frontend/src/{api/types.ts,api/client.ts,
 * pages/LoginPage.tsx,components/CatalogBrowser.tsx,pages/AuditLogPage.tsx,
 * App.tsx} and were left for a follow-up change.
 */
function RequireAuth({ children }: { children: JSX.Element }) {
  const { user } = useAuth();
  if (!user) {
    return <Navigate to="/login" replace />;
  }
  return children;
}

/** Admin-only routes (e.g. the audit log): same isAdmin() check as CatalogBrowser's refresh button. */
function RequireAdmin({ children }: { children: JSX.Element }) {
  const { user } = useAuth();
  if (!user) {
    return <Navigate to="/login" replace />;
  }
  if (!isAdmin(user)) {
    return <Navigate to="/records" replace />;
  }
  return children;
}

export default function App() {
  const { user, logout } = useAuth();

  return (
    <div className="app-shell">
      <nav className="app-nav">
        <Link to="/">tf-automation</Link>
        {user && <Link to="/records">Records</Link>}
        {user && <Link to="/records/new">New record</Link>}
        {user && isAdmin(user) && <Link to="/admin/audit-log">Audit log</Link>}
        <span style={{ flex: 1 }} />
        {user ? (
          <>
            <span>
              {user.username} {isAdmin(user) && '(admin)'}
            </span>
            <button type="button" onClick={() => logout()}>
              Log out
            </button>
          </>
        ) : (
          <Link to="/login">Log in</Link>
        )}
      </nav>

      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/"
          element={
            <RequireAuth>
              <RecordListPage />
            </RequireAuth>
          }
        />
        <Route
          path="/records"
          element={
            <RequireAuth>
              <RecordListPage />
            </RequireAuth>
          }
        />
        <Route
          path="/records/new"
          element={
            <RequireAuth>
              <RecordEditorPage />
            </RequireAuth>
          }
        />
        <Route
          path="/records/:id"
          element={
            <RequireAuth>
              <RecordDetailPage />
            </RequireAuth>
          }
        />
        <Route
          path="/records/:id/edit"
          element={
            <RequireAuth>
              <RecordEditorPage />
            </RequireAuth>
          }
        />
        <Route
          path="/admin/audit-log"
          element={
            <RequireAdmin>
              <AuditLogPage />
            </RequireAdmin>
          }
        />
      </Routes>
    </div>
  );
}
