import { lazy, Suspense } from 'react';
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';

const ShellPage = lazy(() => import('./routes/ShellPage'));
const rootRoute = createRootRoute({
  component: () => (
    <Suspense fallback={<main aria-busy="true">Loading workspace…</main>}>
      <Outlet />
    </Suspense>
  ),
});
const appRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app', component: ShellPage });
const sectionRoutes = ['links', 'qr', 'files', 'analytics', 'domains', 'developer', 'members', 'settings'].map((section) =>
  createRoute({ getParentRoute: () => rootRoute, path: `/app/${section}`, component: ShellPage }),
);
const routeTree = rootRoute.addChildren([appRoute, ...sectionRoutes]);
export const router = createRouter({ routeTree, defaultPreload: 'intent' });
declare module '@tanstack/react-router' { interface Register { router: typeof router } }
