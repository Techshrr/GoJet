import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, EmptyState, InlineMessage } from '@gojet/ui';
import { P12Shell, useP12Authority } from './authority';

const categories = ['all', 'security', 'domains', 'billing', 'support', 'resources'] as const;
type NotificationCategoryFilter = (typeof categories)[number];

export default function NotificationsPage() {
  const authority = useP12Authority();
  const queryClient = useQueryClient();
  const [category, setCategory] = useState<NotificationCategoryFilter>('all');
  const query = useQuery({
    queryKey: ['p12-notifications-page', authority.workspaceId, category],
    enabled: !!authority.client && !!authority.workspaceId,
    queryFn: () => authority.client!.notifications(authority.workspaceId, category),
  });
  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['p12-notifications-page', authority.workspaceId] });
    await queryClient.invalidateQueries({ queryKey: ['p12-notifications', authority.workspaceId] });
    await queryClient.invalidateQueries({ queryKey: ['p12-overview', authority.workspaceId] });
  };
  const mark = useMutation({
    mutationFn: ({ id, read }: { id: number; read: boolean }) => authority.client!.markNotification(authority.workspaceId, id, read),
    onSuccess: invalidate,
  });
  const all = useMutation({ mutationFn: () => authority.client!.markAllNotificationsRead(authority.workspaceId), onSuccess: invalidate });
  const state = query.isPending
    ? 'loading'
    : query.isError
      ? 'error'
      : query.data?.state.status === 'partial' || query.data?.state.status === 'stale'
        ? query.data.state.status
        : category !== 'all'
          ? 'filtered'
          : 'complete';

  return (
    <P12Shell sectionLabel="Notifications">
      <section className="p12-page" data-page="workspace-notifications" data-state={state} data-unread-count={query.data?.unread_count ?? 0}>
        <header className="p12-page-header"><p className="p12-eyebrow">Notification core</p><h1>Notifications</h1><p>Recipient-scoped read state, dedupe and deep-link authorization are server-authoritative.</p></header>
        {query.isError ? <InlineMessage variant="danger">Notifications could not be loaded from the Workspace API.</InlineMessage> : null}
        {query.data && query.data.state.status !== 'complete' ? <InlineMessage variant="warning">Data is {query.data.state.status}: {query.data.state.state_reason}</InlineMessage> : null}
        <div className="p12-page-actions">
          <label>Category<select aria-label="Notification category" value={category} onChange={(event) => setCategory(event.currentTarget.value as NotificationCategoryFilter)}>{categories.map((item) => <option key={item} value={item}>{item === 'all' ? 'All categories' : item}</option>)}</select></label>
          {query.data ? <><strong>{query.data.unread_count} unread</strong><Button onClick={() => all.mutate()} disabled={query.data.unread_count === 0} loading={all.isPending}>Mark all read</Button></> : null}
        </div>
        <div className="p12-list" aria-label="Workspace notifications">{query.data?.items.map((item) => <Card as="article" key={item.id} className={item.read_at ? 'p12-notification-read' : ''} data-notification-category={item.category}><div className="p12-list-row"><div><p className="p12-eyebrow">{item.category}</p><h2>{item.title}</h2><p>{item.summary}</p></div><strong>{item.read_at ? 'Read' : 'Unread'}</strong></div><div className="p12-actions">{item.deep_link ? <a href={item.deep_link}>Open</a> : null}<Button variant="ghost" onClick={() => mark.mutate({ id: item.id, read: !item.read_at })}>Mark {item.read_at ? 'unread' : 'read'}</Button></div></Card>)}</div>
        {query.data?.items.length === 0 ? <EmptyState title="No notifications" reason={category === 'all' ? 'There is no Workspace activity for this recipient.' : 'No notifications match this category.'} /> : null}
      </section>
    </P12Shell>
  );
}
