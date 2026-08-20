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
  LinkAccess,
  LinkCreateInput,
  LinkListFilters,
  LinkListResponse,
  LinkRecord,
  LinkRoutingRule,
  LinkUpdateInput,
  LinkUTM,
  LinkVersionRecord,
} from './links';
