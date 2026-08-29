import { lazy, Suspense } from 'react';
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';
import {
  AdministratorsPage,
  AdminLoginPage,
  AnnouncementsPage,
  AuditPage,
  DomainEntitlementsPage,
  OfficialDomainsPage,
  OperationsJobsPage,
  OperationsServicesPage,
  OverviewPage,
  PlatformGeneralPage,
  RolesPage,
  TurnstilePage,
  UsersPage,
  WorkspacesPage,
} from './p17/pages';
import {
  AdministratorDetailPage,
  DomainEntitlementDetailPage,
  UserDetailPage,
  WorkspaceDetailPage,
} from './p17/details';

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

const login = createRoute({ getParentRoute: () => rootRoute, path: '/admin/login', component: AdminLoginPage });
const home = createRoute({ getParentRoute: () => rootRoute, path: '/admin', component: OverviewPage });
const users = createRoute({ getParentRoute: () => rootRoute, path: '/admin/users', component: UsersPage });
const userDetail = createRoute({ getParentRoute: () => rootRoute, path: '/admin/users/$userId', component: UserDetailPage });
const workspaces = createRoute({ getParentRoute: () => rootRoute, path: '/admin/workspaces', component: WorkspacesPage });
const workspaceDetail = createRoute({ getParentRoute: () => rootRoute, path: '/admin/workspaces/$workspaceId', component: WorkspaceDetailPage });
const domainEntitlements = createRoute({ getParentRoute: () => rootRoute, path: '/admin/domain-entitlements', component: DomainEntitlementsPage });
const domainEntitlementDetail = createRoute({ getParentRoute: () => rootRoute, path: '/admin/domain-entitlements/$workspaceId', component: DomainEntitlementDetailPage });
const administrators = createRoute({ getParentRoute: () => rootRoute, path: '/admin/access/administrators', component: AdministratorsPage });
const administratorDetail = createRoute({ getParentRoute: () => rootRoute, path: '/admin/access/administrators/$adminId', component: AdministratorDetailPage });
const roles = createRoute({ getParentRoute: () => rootRoute, path: '/admin/access/roles', component: RolesPage });
const audit = createRoute({ getParentRoute: () => rootRoute, path: '/admin/audit', component: AuditPage });
const operationJobs = createRoute({ getParentRoute: () => rootRoute, path: '/admin/operations/jobs', component: OperationsJobsPage });
const operationServices = createRoute({ getParentRoute: () => rootRoute, path: '/admin/operations/services', component: OperationsServicesPage });
const general = createRoute({ getParentRoute: () => rootRoute, path: '/admin/platform/general', component: PlatformGeneralPage });
const officialDomains = createRoute({ getParentRoute: () => rootRoute, path: '/admin/platform/official-domains', component: OfficialDomainsPage });
const turnstile = createRoute({ getParentRoute: () => rootRoute, path: '/admin/platform/turnstile', component: TurnstilePage });
const announcements = createRoute({ getParentRoute: () => rootRoute, path: '/admin/announcements', component: AnnouncementsPage });
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
const sections = ['customers', 'resources', 'trust-safety', 'operations', 'commerce', 'access', 'platform'].map((section) =>
  createRoute({ getParentRoute: () => rootRoute, path: `/admin/${section}`, component: ShellPage }),
);

const routeTree = rootRoute.addChildren([
  login,
  home,
  users,
  userDetail,
  workspaces,
  workspaceDetail,
  domainEntitlements,
  domainEntitlementDetail,
  administrators,
  administratorDetail,
  roles,
  audit,
  operationJobs,
  operationServices,
  general,
  officialDomains,
  turnstile,
  announcements,
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
