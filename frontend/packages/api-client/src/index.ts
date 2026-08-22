import { QueryClient } from '@tanstack/react-query';

export type ApiProblem = {
  code: string;
  message: string;
  requestId?: string;
};

export function createGoJetQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: 1,
        staleTime: 30_000,
        refetchOnWindowFocus: false,
      },
      mutations: { retry: 0 },
    },
  });
}

export {
  GoJetApiError,
  GoJetLinksClient,
} from './links';
export type {
  ApiTransport,
  BulkLinkAction,
  BulkLinkResponse,
  BulkLinkResult,
  LinkABVariant,
  LinkAccessInput,
  LinkAccessState,
  LinkCreateInput,
  LinkListFilters,
  LinkListResponse,
  LinkRecord,
  LinkRoutingRule,
  LinkUpdateInput,
  LinkUTM,
  LinkVersionRecord,
} from './links';

export { GoJetDomainsClient } from './domains';
export type {
  CreatedDomainRecord,
  CreatedWorkspaceDomain,
  CreateWorkspaceDomainInput,
  DomainAssignedLink,
  DomainEntitlementSource,
  DomainEntitlementStatus,
  DomainHTTPSStatus,
  DomainIngressDNSStatus,
  DomainOwnershipStatus,
  DomainRevalidationRecord,
  DomainRiskStatus,
  DomainRoutingState,
  WorkspaceDomainDetailResponse,
  WorkspaceDomainEntitlement,
  WorkspaceDomainRecord,
  WorkspaceDomainsResponse,
  WorkspaceDomainViewState,
} from './domains';

export { GoJetAnalyticsClient } from './analytics';
export type {
  AnalyticsBucket,
  AnalyticsConversionInput,
  AnalyticsConversionResponse,
  AnalyticsDimensionCount,
  AnalyticsGranularity,
  AnalyticsQueryInput,
  AnalyticsReport,
  AnalyticsState,
} from './analytics';

export { GoJetQRClient } from './qr';
export type {
  QRArtifact,
  QRCreateInput,
  QRListResponse,
  QRRiskState,
  QRRecord,
  QRSource,
  QRState,
} from './qr';
