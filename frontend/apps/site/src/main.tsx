import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { createGoJetQueryClient } from '@gojet/api-client';
import { router } from './router';
import '@gojet/ui/styles.css';
import './shell/shell.css';
import './website/website.css';
import './contact/contact.css';
import './auth/auth.css';
import './trust/trust.css';

const container = document.getElementById('root');
if (!container) throw new Error('GoJet application root is missing');
const queryClient = createGoJetQueryClient();

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
