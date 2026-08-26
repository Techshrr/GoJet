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

export { GoJetApiError, GoJetLinksClient } from './links';
export type {
  ApiTransport, BulkLinkAction, BulkLinkResponse, BulkLinkResult, LinkABVariant, LinkAccessInput,
  LinkAccessState, LinkCreateInput, LinkListFilters, LinkListResponse, LinkRecord, LinkRoutingRule,
  LinkUpdateInput, LinkUTM, LinkVersionRecord,
} from './links';

export { GoJetDomainsClient } from './domains';
export type {
  CreatedDomainRecord, CreatedWorkspaceDomain, CreateWorkspaceDomainInput, DomainAssignedLink,
  DomainEntitlementSource, DomainEntitlementStatus, DomainHTTPSStatus, DomainIngressDNSStatus,
  DomainOwnershipStatus, DomainRevalidationRecord, DomainRiskStatus, DomainRoutingState,
  WorkspaceDomainDetailResponse, WorkspaceDomainEntitlement, WorkspaceDomainRecord,
  WorkspaceDomainsResponse, WorkspaceDomainViewState,
} from './domains';

export { GoJetAnalyticsClient } from './analytics';
export type {
  AnalyticsBucket, AnalyticsConversionInput, AnalyticsConversionResponse, AnalyticsDimensionCount,
  AnalyticsGranularity, AnalyticsQueryInput, AnalyticsReport, AnalyticsState,
} from './analytics';

export { GoJetQRClient } from './qr';
export type { QRArtifact, QRCreateInput, QRListResponse, QRRiskState, QRRecord, QRSource, QRState } from './qr';

export { GoJetFilesClient } from './files';
export type {
  FileArtifact, FileDependencyHealthReport, FileDependencyState, FileListResponse, FilePolicyInput,
  FileRecord, FileScanState, FileUploadInput,
} from './files';

export { GoJetTextClient } from './text';
export type { TextCreateInput, TextListResponse, TextShareRecord, TextUpdateInput, TextVisibility } from './text';

export { GoJetBioClient } from './bio';
export type {
  BioChildInput, BioChildRecord, BioChildRiskStatus, BioCreateInput, BioListResponse,
  BioPageRecord, BioStatus, BioUpdateInput,
} from './bio';

export { GoJetWorkspaceClient } from './workspace';
export type {
  CreatedWorkspaceInvitation, InvitationInspection, WorkspaceCampaign, WorkspaceContext, WorkspaceFolder,
  WorkspaceInvitation, WorkspaceMembership, WorkspaceMembersResponse, WorkspaceNotification,
  WorkspaceNotificationState, WorkspaceNotificationsResponse, WorkspaceOrganization, WorkspaceOverview,
  WorkspaceRecord, WorkspaceRole, WorkspaceTag,
} from './workspace';

export { GoJetSupportClient } from './support';
export type {
  SupportActorType, SupportMessage, SupportMessageKind, SupportTicket, SupportTicketCloseResponse,
  SupportTicketCreateInput, SupportTicketCreateResponse, SupportTicketDetailResponse,
  SupportTicketListResponse, SupportTicketReplyResponse, SupportTicketStatus,
} from './support';

export { GoJetAuthClient } from './auth';
export type {
  AuthProvider, AuthProvidersResponse, AuthStatusResponse, OAuthCallbackResponse, OAuthHandoffResponse,
  SocialRegistrationState,
} from './auth';
