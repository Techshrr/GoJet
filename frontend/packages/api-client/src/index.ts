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
