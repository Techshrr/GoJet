import { lazy, Suspense } from 'react';
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';

const ShellPage = lazy(() => import('./routes/ShellPage'));
const rootRoute = createRootRoute({
  component: () => (
    <Suspense fallback={<main aria-busy="true">Loading admin…</main>}>
      <Outlet />
    </Suspense>
  ),
});
const home = createRoute({ getParentRoute: () => rootRoute, path: '/admin', component: ShellPage });
const sections = ['customers', 'resources', 'trust-safety', 'operations', 'commerce', 'access', 'platform', 'audit'].map((section) =>
  createRoute({ getParentRoute: () => rootRoute, path: `/admin/${section}`, component: ShellPage }),
);
const routeTree = rootRoute.addChildren([home, ...sections]);
export const router = createRouter({ routeTree, defaultPreload: 'intent' });
declare module '@tanstack/react-router' { interface Register { router: typeof router } }
