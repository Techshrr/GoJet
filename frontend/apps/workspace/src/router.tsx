import { lazy, Suspense } from 'react';
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';
import {
  CampaignsPage,
  InvitationPage,
  OrganizationPage,
  TagsPage,
  WorkspaceOverviewPage,
  WorkspaceSettingsPage,
} from './workspace/pages';
import MembersPage from './workspace/MembersPage';
import NotificationsPage from './workspace/NotificationsPage';
import { SupportListPage, SupportNewPage, SupportThreadPage } from './support/pages';

const ShellPage = lazy(() => import('./routes/ShellPage'));
const LinksListPage = lazy(() => import('./routes/LinksListPage'));
const LinkCreatePage = lazy(() => import('./routes/LinkCreatePage'));
const LinkDetailPage = lazy(() => import('./routes/LinkDetailPage'));
const DomainsListPage = lazy(() => import('./routes/DomainsListPage'));
const DomainCreatePage = lazy(() => import('./routes/DomainCreatePage'));
const DomainDetailPage = lazy(() => import('./routes/DomainDetailPage'));
const AnalyticsPage = lazy(() => import('./routes/AnalyticsPage'));
const QRListPage = lazy(() => import('./routes/QRListPage'));
const QRDetailPage = lazy(() => import('./routes/QRDetailPage'));
const FilesListPage = lazy(() => import('./routes/FilesListPage'));
const FileDetailPage = lazy(() => import('./routes/FileDetailPage'));
const TextListPage = lazy(() => import('./routes/TextListPage'));
const TextDetailPage = lazy(() => import('./routes/TextDetailPage'));
const BioListPage = lazy(() => import('./routes/BioListPage'));
const BioDetailPage = lazy(() => import('./routes/BioDetailPage'));
const BillingPage = lazy(() => import('./routes/BillingPage'));

const rootRoute = createRootRoute({
  component: () => (
    <Suspense fallback={<main aria-busy="true">Loading workspace…</main>}>
      <Outlet />
    </Suspense>
  ),
});

const appRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app', component: WorkspaceOverviewPage });
const linksRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/links', component: LinksListPage });
const linkCreateRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/links/new', component: LinkCreatePage });
const linkDetailRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/links/$linkId', component: LinkDetailPage });
const domainsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/domains', component: DomainsListPage });
const domainCreateRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/domains/new', component: DomainCreatePage });
const domainDetailRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/domains/$domainId', component: DomainDetailPage });
const analyticsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/analytics', component: AnalyticsPage });
const qrRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/qr', component: QRListPage });
const qrDetailRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/qr/$qrId', component: QRDetailPage });
const filesRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/files', component: FilesListPage });
const fileDetailRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/files/$fileId', component: FileDetailPage });
const textRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/text', component: TextListPage });
const textDetailRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/text/$shareId', component: TextDetailPage });
const bioRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/bio', component: BioListPage });
const bioDetailRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/bio/$pageId', component: BioDetailPage });
const billingRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/billing', component: BillingPage });
const supportRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/support', component: SupportListPage });
const supportNewRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/support/new', component: SupportNewPage });
const supportThreadRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/support/$ticketId', component: SupportThreadPage });
const notificationsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/notifications', component: NotificationsPage });
const organizationRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/organization', component: OrganizationPage });
const campaignsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/campaigns', component: CampaignsPage });
const tagsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/tags', component: TagsPage });
const membersRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/members', component: MembersPage });
const workspaceSettingsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/app/settings/workspace', component: WorkspaceSettingsPage });
const inviteRoute = createRoute({ getParentRoute: () => rootRoute, path: '/invite/$token', component: InvitationPage });
const sectionRoutes = ['developer', 'settings'].map((section) =>
  createRoute({ getParentRoute: () => rootRoute, path: `/app/${section}`, component: ShellPage }),
);
const routeTree = rootRoute.addChildren([
  appRoute, linksRoute, linkCreateRoute, linkDetailRoute, domainsRoute, domainCreateRoute, domainDetailRoute,
  analyticsRoute, qrRoute, qrDetailRoute, filesRoute, fileDetailRoute, textRoute, textDetailRoute, bioRoute, bioDetailRoute,
  billingRoute, supportRoute, supportNewRoute, supportThreadRoute, notificationsRoute, organizationRoute, campaignsRoute, tagsRoute, membersRoute, workspaceSettingsRoute, inviteRoute,
  ...sectionRoutes,
]);
export const router = createRouter({ routeTree, defaultPreload: 'intent' });
declare module '@tanstack/react-router' { interface Register { router: typeof router } }
