import { lazy, Suspense } from 'react';
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';

const FoundationPage = lazy(() => import('./routes/FoundationPage'));
const rootRoute = createRootRoute({
  component: () => (
    <Suspense fallback={<main aria-busy="true">Loading GoJet foundation…</main>}>
      <Outlet />
    </Suspense>
  ),
});
const foundationRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app', component: FoundationPage });
const routeTree = rootRoute.addChildren([foundationRoute]);
export const router = createRouter({ routeTree, defaultPreload: 'intent' });
declare module '@tanstack/react-router' { interface Register { router: typeof router; } }
