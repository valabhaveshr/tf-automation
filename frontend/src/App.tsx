import type { ReactNode } from 'react';
import { Center, Loader } from '@mantine/core';
import { Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { useAuth } from './state/auth';
import AppLayout from './layout/AppLayout';
import LoginPage from './pages/LoginPage';
import AcceptInvitePage from './pages/AcceptInvitePage';
import OnboardingPage from './pages/OnboardingPage';
import NoOrganizationPage from './pages/NoOrganizationPage';
import ResourcesPage from './pages/ResourcesPage';
import ResourceEditorPage from './pages/ResourceEditorPage';
import ResourceDetailPage from './pages/ResourceDetailPage';
import CatalogPage from './pages/CatalogPage';
import TeamPage from './pages/TeamPage';
import OrganizationPage from './pages/OrganizationPage';
import CustomersPage from './pages/CustomersPage';
import AuditLogPage from './pages/AuditLogPage';

/**
 * Routing and session guards:
 *   no session              -> /login (invite links stay public)
 *   invited customer admin  -> /onboarding until the organization exists
 *   no organization at all  -> "ask for an invitation" screen
 *   otherwise               -> the app; platform-admin pages are role-gated
 *                              here and again server-side.
 */
export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/invite/:token" element={<AcceptInvitePage />} />
      <Route
        path="/onboarding"
        element={
          <RequireSession allowOnboarding>
            <OnboardingPage />
          </RequireSession>
        }
      />
      <Route path="/*" element={<RequireSession>{<AppRoutes />}</RequireSession>} />
    </Routes>
  );
}

function AppRoutes() {
  const { me } = useAuth();
  const platform = !!me?.is_platform_admin;
  return (
    <AppLayout>
      <Routes>
        <Route path="/" element={<ResourcesPage />} />
        <Route path="/resources/new" element={<ResourceEditorPage />} />
        <Route path="/resources/:id" element={<ResourceDetailPage />} />
        <Route path="/resources/:id/edit" element={<ResourceEditorPage />} />
        <Route path="/catalog" element={<CatalogPage />} />
        {!platform && <Route path="/team" element={<TeamPage />} />}
        {!platform && <Route path="/organization" element={<OrganizationPage />} />}
        {platform && <Route path="/admin/customers" element={<CustomersPage />} />}
        {platform && <Route path="/admin/audit-log" element={<AuditLogPage />} />}
        {/* Old "records" URLs from before the rename. */}
        <Route path="/records" element={<Navigate to="/" replace />} />
        <Route path="/records/new" element={<Navigate to="/resources/new" replace />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </AppLayout>
  );
}

function RequireSession({ children, allowOnboarding }: { children: ReactNode; allowOnboarding?: boolean }) {
  const { me, initializing } = useAuth();
  const location = useLocation();

  if (initializing) {
    return (
      <Center h="100vh">
        <Loader />
      </Center>
    );
  }
  if (!me) return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  if (me.needs_onboarding && !allowOnboarding) return <Navigate to="/onboarding" replace />;
  if (!me.needs_onboarding && allowOnboarding) return <Navigate to="/" replace />;
  if (!me.customer && !me.is_platform_admin && !me.needs_onboarding) return <NoOrganizationPage />;
  return <>{children}</>;
}
