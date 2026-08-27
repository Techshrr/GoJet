import { Card, InlineMessage } from '@gojet/ui';
import { Link } from '@tanstack/react-router';
import { WebsiteShell } from '../shell/SiteShells';
import { usePrivatePublicPage } from './pageSecurity';

type SafetyCopy = { title: string; summary: string; guidance: string };

const safetyReasons = {
  pending: {
    title: 'This link is being checked',
    summary: 'GoJet has not approved the current destination yet.',
    guidance: 'Try again later. The destination remains unavailable until the current safety check completes.',
  },
  review: {
    title: 'This link needs a safety review',
    summary: 'The current destination requires additional review before it can be opened.',
    guidance: 'Try again later or contact the person who shared the link if you need another way to reach the resource.',
  },
  blocked: {
    title: 'This link is unavailable for safety reasons',
    summary: 'GoJet is not allowing this destination to open.',
    guidance: 'Do not attempt to bypass this safety page. You can report the link if you believe it is abusive or unsafe.',
  },
  missing: {
    title: 'This link cannot be verified',
    summary: 'A current safety decision is not available for this destination.',
    guidance: 'The link remains unavailable until GoJet can establish current safety authority.',
  },
  malformed: {
    title: 'This link cannot be checked safely',
    summary: 'The destination did not meet the requirements for a safe destination check.',
    guidance: 'The link remains unavailable. Contact the person who shared it if you need a corrected link.',
  },
  stale: {
    title: 'This link changed and must be checked again',
    summary: 'A previous safety decision no longer applies to the current destination configuration.',
    guidance: 'Try again after the new destination has completed its safety check.',
  },
  'domain-suspended': {
    title: 'This domain is temporarily unavailable',
    summary: 'The custom domain used by this link is suspended.',
    guidance: 'The link cannot continue through another host while the domain safety restriction is active.',
  },
  'domain-revoked': {
    title: 'This domain is no longer authorized',
    summary: 'The custom domain no longer has current authorization for link routing.',
    guidance: 'Contact the person who shared the link if you need an updated address.',
  },
  'domain-expired': {
    title: 'This domain is unavailable',
    summary: 'The custom domain no longer has active access for this link.',
    guidance: 'Contact the person who shared the link if you need an updated address.',
  },
  'operational-unavailable': {
    title: 'This link is temporarily unavailable',
    summary: 'GoJet cannot establish the required safety authority right now.',
    guidance: 'Try again later. The link fails closed while required safety services are unavailable.',
  },
  'service-unavailable': {
    title: 'This link is temporarily unavailable',
    summary: 'GoJet cannot establish the required safety authority right now.',
    guidance: 'Try again later. The link fails closed while required safety services are unavailable.',
  },
} satisfies Record<string, SafetyCopy>;

type SafetyReason = keyof typeof safetyReasons;

function readReason(): SafetyReason {
  const raw = new URLSearchParams(window.location.search).get('reason')?.trim().toLowerCase() ?? '';
  return Object.prototype.hasOwnProperty.call(safetyReasons, raw) ? raw as SafetyReason : 'operational-unavailable';
}

function readSafeCode(): string {
  const raw = new URLSearchParams(window.location.search).get('code')?.trim() ?? '';
  return /^[A-Za-z0-9._~-]{1,128}$/.test(raw) ? raw : '';
}

export default function LinkUnavailablePage() {
  usePrivatePublicPage('Link unavailable · GoJet');
  const reason = readReason();
  const code = readSafeCode();
  const copy = safetyReasons[reason];

  return <WebsiteShell>
    <section className="public-trust-page public-safety-page" data-page="linkunavailable" data-state={reason}>
      <header className="public-trust-hero">
        <p className="public-trust-eyebrow">LINK SAFETY</p>
        <h1>{copy.title}</h1>
        <p>{copy.summary}</p>
      </header>
      <InlineMessage variant={reason === 'blocked' || reason === 'domain-suspended' || reason === 'domain-revoked' ? 'danger' : 'warning'}>
        {copy.guidance}
      </InlineMessage>
      <Card as="section" className="public-trust-card" aria-labelledby="safety-boundary-title">
        <h2 id="safety-boundary-title">What this page does</h2>
        <p>This page intentionally does not reveal the destination URL, safety provider, detection evidence, scoring threshold, or any bypass path.</p>
        {code ? <p className="public-trust-reference"><span>Link reference</span><strong>{code}</strong></p> : null}
      </Card>
      <div className="public-trust-actions">
        <Link to="/abuse/report" className="site-primary-link">Report abuse</Link>
        <Link to="/">Go to GoJet</Link>
      </div>
    </section>
  </WebsiteShell>;
}
