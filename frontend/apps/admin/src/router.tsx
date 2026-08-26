import { lazy, Suspense } from 'react';
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';

const ShellPage = lazy(() => import('./routes/ShellPage'));
const StorageStatusPage = lazy(() => import('./routes/StorageStatusPage'));
const CommercePlansPage = lazy(() => import('./routes/CommercePlansPage'));
const CommercePaymentsPage = lazy(() => import('./routes/CommercePaymentsPage'));
const CommercePaymentDetailPage = lazy(() => import('./routes/CommercePaymentDetailPage'));
const CommerceFXPage = lazy(() => import('./routes/CommerceFXPage'));
const AdminTicketsPage = lazy(() => import('./support/pages').then((module) => ({ default: module.AdminTicketsPage })));
const AdminTicketDetailPage = lazy(() => import('./support/pages').then((module) => ({ default: module.AdminTicketDetailPage })));
const AdminMailPage = lazy(() => import('./support/pages').then((module) => ({ default: module.AdminMailPage })));
const OAuthAdminPage = lazy(() => import('./oauth/OAuthAdminPage'));
const DestinationRiskListPage = lazy(() => import('./trust/DestinationRiskPage'));
const DestinationRiskDetailPage = lazy(() => import('./trust/DestinationRiskPage').then((module) => ({ default: module.DestinationRiskDetailPage })));
const DomainRiskListPage = lazy(() => import('./trust/DomainRiskPage'));
const DomainRiskDetailPage = lazy(() => import('./trust/DomainRiskPage').then((module) => ({ default: module.DomainRiskDetailPage })));
const AbuseListPage = lazy(() => import('./trust/AbusePage'));
const AbuseDetailPage = lazy(() => import('./trust/AbusePage').then((module) => ({ default: module.AbuseDetailPage })));

const rootRoute = createRootRoute({
  component: () => (
    <Suspense fallback={<main aria-busy="true">Loading admin…</main>}>
      <Outlet />
    </Suspense>
  ),
});

const home = createRoute({ getParentRoute: () => rootRoute, path: '/admin', component: ShellPage });
const storageStatus = createRoute({ getParentRoute: () => rootRoute, path: '/admin/platform/storage', component: StorageStatusPage });
const oauthAdmin = createRoute({ getParentRoute: () => rootRoute, path: '/admin/platform/oauth', component: OAuthAdminPage });
const commercePlans = createRoute({ getParentRoute: () => rootRoute, path: '/admin/commerce/plans', component: CommercePlansPage });
const commercePayments = createRoute({ getParentRoute: () => rootRoute, path: '/admin/commerce/payments', component: CommercePaymentsPage });
const commercePaymentDetail = createRoute({ getParentRoute: () => rootRoute, path: '/admin/commerce/payments/$paymentId', component: CommercePaymentDetailPage });
const commerceFX = createRoute({ getParentRoute: () => rootRoute, path: '/admin/commerce/fx', component: CommerceFXPage });
const adminTickets = createRoute({ getParentRoute: () => rootRoute, path: '/admin/tickets', component: AdminTicketsPage });
const adminTicketDetail = createRoute({ getParentRoute: () => rootRoute, path: '/admin/tickets/$ticketId', component: AdminTicketDetailPage });
const adminMail = createRoute({ getParentRoute: () => rootRoute, path: '/admin/mail', component: AdminMailPage });
const destinationRiskList = createRoute({ getParentRoute: () => rootRoute, path: '/admin/trust/destination-risk', component: DestinationRiskListPage });
const destinationRiskDetail = createRoute({ getParentRoute: () => rootRoute, path: '/admin/trust/destination-risk/$riskId', component: DestinationRiskDetailPage });
const domainRiskList = createRoute({ getParentRoute: () => rootRoute, path: '/admin/trust/domain-risk', component: DomainRiskListPage });
const domainRiskDetail = createRoute({ getParentRoute: () => rootRoute, path: '/admin/trust/domain-risk/$domainId', component: DomainRiskDetailPage });
const abuseList = createRoute({ getParentRoute: () => rootRoute, path: '/admin/trust/abuse', component: AbuseListPage });
const abuseDetail = createRoute({ getParentRoute: () => rootRoute, path: '/admin/trust/abuse/$reportId', component: AbuseDetailPage });
const sections = ['customers', 'resources', 'trust-safety', 'operations', 'commerce', 'access', 'platform', 'audit'].map((section) =>
  createRoute({ getParentRoute: () => rootRoute, path: `/admin/${section}`, component: ShellPage }),
);

const routeTree = rootRoute.addChildren([
  home,
  storageStatus,
  oauthAdmin,
  commercePlans,
  commercePayments,
  commercePaymentDetail,
  commerceFX,
  adminTickets,
  adminTicketDetail,
  adminMail,
  destinationRiskList,
  destinationRiskDetail,
  domainRiskList,
  domainRiskDetail,
  abuseList,
  abuseDetail,
  ...sections,
]);

export const router = createRouter({ routeTree, defaultPreload: 'intent' });
declare module '@tanstack/react-router' { interface Register { router: typeof router } }
