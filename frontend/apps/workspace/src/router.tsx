import { lazy, Suspense } from 'react';
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';

const ShellPage = lazy(() => import('./routes/ShellPage'));
const LinksListPage = lazy(() => import('./routes/LinksListPage'));
const LinkCreatePage = lazy(() => import('./routes/LinkCreatePage'));
const LinkDetailPage = lazy(() => import('./routes/LinkDetailPage'));
const DomainsListPage = lazy(() => import('./routes/DomainsListPage'));
const DomainCreatePage = lazy(() => import('./routes/DomainCreatePage'));
const DomainDetailPage = lazy(() => import('./routes/DomainDetailPage'));

const rootRoute = createRootRoute({
  component: () => (
    <Suspense fallback={<main aria-busy="true">Loading workspace…</main>}>
      <Outlet />
    </Suspense>
  ),
});

const appRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app', component: ShellPage });
const linksRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/links', component: LinksListPage });
const linkCreateRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/links/new', component: LinkCreatePage });
const linkDetailRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/links/$linkId', component: LinkDetailPage });
const domainsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/domains', component: DomainsListPage });
const domainCreateRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/domains/new', component: DomainCreatePage });
const domainDetailRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/domains/$domainId', component: DomainDetailPage });
const sectionRoutes = ['qr', 'files', 'analytics', 'developer', 'members', 'settings'].map((section) =>
  createRoute({ getParentRoute: () => rootRoute, path: `/app/${section}`, component: ShellPage }),
);
const routeTree = rootRoute.addChildren([
  appRoute,
  linksRoute,
  linkCreateRoute,
  linkDetailRoute,
  domainsRoute,
  domainCreateRoute,
  domainDetailRoute,
  ...sectionRoutes,
]);
export const router = createRouter({ routeTree, defaultPreload: 'intent' });
declare module '@tanstack/react-router' { interface Register { router: typeof router } }
