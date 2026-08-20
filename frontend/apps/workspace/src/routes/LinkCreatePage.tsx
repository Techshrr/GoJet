import { type FormEvent, useMemo, useState } from 'react';
import { Link, useNavigate } from '@tanstack/react-router';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError, type LinkCreateInput } from '@gojet/api-client';
import { Button, Card, Checkbox, InlineMessage, SelectField, TextField } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { createWorkspaceLinksClient, isReadOnly, readWorkspaceRuntime } from '../links/runtime';

export default function LinkCreatePage() {
  const runtime = useMemo(() => readWorkspaceRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceLinksClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const readOnly = runtime ? isReadOnly(runtime) : true;

  const [destination, setDestination] = useState('');
  const [domainKind, setDomainKind] = useState<'official' | 'custom'>('official');
  const [hostname, setHostname] = useState(import.meta.env.VITE_GOJET_TEST_SHORT_HOST?.trim() ?? '');
  const [code, setCode] = useState('');
  const [title, setTitle] = useState('');
  const [redirectStatus, setRedirectStatus] = useState<LinkCreateInput['redirect_status']>(302);
  const [expiresAt, setExpiresAt] = useState('');
  const [clickLimit, setClickLimit] = useState('');
  const [oneTime, setOneTime] = useState(false);
  const [changeReason, setChangeReason] = useState('Create link');

  const mutation = useMutation({
    mutationFn: async () => {
      if (!client || !runtime) throw new Error('Workspace authority unavailable');
      const limit = clickLimit.trim() ? Number(clickLimit) : null;
      const input: LinkCreateInput = {
        hostname: hostname.trim(),
        domain_kind: domainKind,
        code: code.trim(),
        title: title.trim(),
        primary_destination: destination.trim(),
        redirect_status: redirectStatus,
        routing: [],
        ab: [],
        utm: {},
        access: {},
        expires_at: expiresAt ? new Date(expiresAt).toISOString() : null,
        click_limit: limit,
        one_time: oneTime,
        change_reason: changeReason.trim(),
      };
      return client.create(runtime.workspaceId, input);
    },
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ['links', runtime?.workspaceId] });
      await navigate({ to: '/app/links/$linkId', params: { linkId: String(created.id) } });
    },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!readOnly) mutation.mutate();
  }

  const apiError = mutation.error instanceof GoJetApiError ? mutation.error : null;

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="Create link">
      <section className="links-page" data-page="link-create">
        <header className="links-page-header">
          <div><p className="links-eyebrow">LINKS</p><h1>Create link</h1><p>Create the base link first. Routing and A/B are configured from Link Detail after creation.</p></div>
          <Link to="/app/links" className="links-secondary-link">Back to links</Link>
        </header>
        {!runtime ? <InlineMessage variant="warning">Production Workspace identity is not available until P12/P15 provides the authoritative context.</InlineMessage> : null}
        {apiError ? <InlineMessage variant={apiError.status === 409 ? 'warning' : 'danger'}>{apiError.message} <strong>{apiError.code}</strong></InlineMessage> : null}

        <form className="links-form" onSubmit={submit}>
          <Card as="section" className="links-form-section">
            <h2>Destination and short URL</h2>
            <TextField id="link-destination" label="Destination URL" type="url" required value={destination} onChange={(event) => setDestination(event.currentTarget.value)} placeholder="https://example.com/page" helpText="Changing a reachable destination creates a new safety fingerprint." />
            <SelectField id="link-domain-kind" label="Domain authority" value={domainKind} onChange={(event) => setDomainKind(event.currentTarget.value as 'official' | 'custom')} options={[
              { value: 'official', label: 'Official GoJet domain' },
              { value: 'custom', label: 'Custom domain — requires P06 authority' },
            ]} />
            <TextField id="link-hostname" label="Hostname" required value={hostname} onChange={(event) => setHostname(event.currentTarget.value)} placeholder="go.example.com" helpText={domainKind === 'custom' ? 'P05 fails closed until P06 confirms entitlement, ownership, DNS, HTTPS and domain risk.' : 'Use an eligible official hostname.'} />
            <TextField id="link-code" label="Custom code" required value={code} onChange={(event) => setCode(event.currentTarget.value)} placeholder="launch" pattern="(?:[A-Za-z0-9_]|-)+" />
            <TextField id="link-title" label="Title" value={title} onChange={(event) => setTitle(event.currentTarget.value)} placeholder="Campaign landing page" />
            <SelectField id="link-redirect-status" label="Redirect status" value={String(redirectStatus)} onChange={(event) => setRedirectStatus(Number(event.currentTarget.value) as LinkCreateInput['redirect_status'])} options={[
              { value: '301', label: '301 — Permanent' },
              { value: '302', label: '302 — Temporary' },
              { value: '307', label: '307 — Temporary, preserve method' },
              { value: '308', label: '308 — Permanent, preserve method' },
            ]} />
          </Card>

          <Card as="section" className="links-form-section">
            <h2>Advanced access lifecycle</h2>
            <TextField id="link-expires" label="Expiration" type="datetime-local" value={expiresAt} onChange={(event) => setExpiresAt(event.currentTarget.value)} />
            <TextField id="link-click-limit" label="Click limit" type="number" min="1" step="1" value={clickLimit} onChange={(event) => setClickLimit(event.currentTarget.value)} placeholder="No limit" />
            <Checkbox label="One-time link" checked={oneTime} onChange={(event) => setOneTime(event.currentTarget.checked)} />
            <TextField id="link-password-owner" label="Password protection" disabled value="" helpText="Server-side password hashing and challenge enforcement must be completed before this control can be enabled; P05 does not send password hashes from the browser." />
            <SelectField id="link-campaign-owner" label="Campaign" disabled value="" options={[{ value: '', label: 'Campaign authority not yet available' }]} helpText="Campaign records are owned by the Campaign/Analytics node." />
            <SelectField id="link-tags-owner" label="Tags" disabled value="" options={[{ value: '', label: 'Tag authority not yet available' }]} helpText="Workspace tag records are owned by P12." />
            <TextField id="link-change-reason" label="Change reason" required value={changeReason} onChange={(event) => setChangeReason(event.currentTarget.value)} helpText="Stored with immutable version and audit history." />
          </Card>

          <div className="links-form-actions">
            <Link to="/app/links" className="links-secondary-link">Cancel</Link>
            <Button type="submit" loading={mutation.isPending} disabled={readOnly || !destination.trim() || !hostname.trim() || !code.trim() || !changeReason.trim()}>Create link</Button>
          </div>
        </form>
      </section>
    </WorkspaceShell>
  );
}
