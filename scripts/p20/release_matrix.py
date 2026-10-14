"""Exact-head whole-product regression admission, separate from historical signing.

Read-only helper for final P20 closure. Merely listing workflows never proves PASS.
"""
from pathlib import Path
from t037_case import require

# Fixed reviewed inventory: new or removed predecessor workflows require disposition.
REQUIRED_PREDECESSORS = (
    'p00-bootstrap.yml',
    'p01-engineering.yml',
    'p02-brand-foundation.yml',
    'p03-design-system.yml',
    'p04-product-shells.yml',
    'p05-browser.yml',
    'p05-integration.yml',
    'p05-links-domain-contract.yml',
    'p06-browser.yml',
    'p06-custom-domains.yml',
    'p06-integration.yml',
    'p07-analytics.yml',
    'p07-browser.yml',
    'p07-integration.yml',
    'p08-browser.yml',
    'p08-evidence.yml',
    'p08-integration.yml',
    'p08-qr.yml',
    'p09-browser.yml',
    'p09-evidence.yml',
    'p09-files.yml',
    'p09-health.yml',
    'p09-integration.yml',
    'p10-browser.yml',
    'p10-evidence.yml',
    'p10-integration.yml',
    'p10-text.yml',
    'p11-bio.yml',
    'p11-browser.yml',
    'p11-evidence.yml',
    'p11-integration.yml',
    'p12-browser.yml',
    'p12-evidence.yml',
    'p12-integration.yml',
    'p12-workspace-organization.yml',
    'p13-billing-payments-entitlements.yml',
    'p13-browser.yml',
    'p13-evidence.yml',
    'p13-integration.yml',
    'p14-browser-023.yml',
    'p14-browser.yml',
    'p14-integration.yml',
    'p14-support-tickets-mail.yml',
    'p15-account-browser.yml',
    'p15-account-oauth-integration.yml',
    'p15-admin-oauth-browser.yml',
    'p15-auth-security-integration.yml',
    'p15-authentication-oauth-account.yml',
    'p15-browser.yml',
    'p15-handoff-mail-audit-integration.yml',
    'p15-real-auth-integration.yml',
    'p16-admin-risk-api.yml',
    'p16-browser.yml',
    'p16-destination-risk-integration.yml',
    'p16-evidence.yml',
    'p16-notification-producer.yml',
    'p16-trust-destination-risk-abuse.yml',
    'p17-admin-access-foundation.yml',
    'p17-admin-access-integration.yml',
    'p17-admin-permissions-audit.yml',
    'p17-api-key-governance-integration.yml',
    'p17-browser.yml',
    'p17-domain-entitlement-integration.yml',
    'p17-evidence.yml',
    'p17-platform-governance-integration.yml',
    'p17-resource-operations-integration.yml',
    'p17-user-workspace-governance-integration.yml',
    'p17-webhook-governance-integration.yml',
    'p18-docs-core.yml',
    'p18-docs-discovery.yml',
    'p18-docs-evidence.yml',
    'p18-docs-multilingual-discovery.yml',
    'p18-docs-quality.yml',
    'p19-website-browser.yml',
    'p19-website-content.yml',
    'p19-website-core.yml',
    'p19-website-discovery.yml',
    'p19-website-evidence.yml',
    'p19-website-quality.yml',
    'p19-website-technical-seo.yml',
)
HISTORICAL = (
    'p04-historical-replay.yml',
    'p05-closure.yml',
    'p06-closure.yml',
    'p07-closure.yml',
    'p08-closure.yml',
    'p09-closure.yml',
    'p10-closure.yml',
    'p11-closure.yml',
    'p12-closure.yml',
    'p13-closure.yml',
    'p16-closure.yml',
    'p17-closure.yml',
    'p18-closure.yml',
    'p19-closure.yml',
)
REQUIRED_P20 = (
    'p20-whole-product-verification.yml', 'p20-candidate-freeze.yml',
    'p20-p0-support.yml', 'p20-t027-consistency.yml',
    'p20-t028-consistency.yml', 'p20-t029-consistency.yml',
    'p20-t030-consistency.yml', 'p20-t031-consistency.yml',
    'p20-t032-consistency.yml', 'p20-t033-consistency.yml',
    'p20-t034-matrix.yml', 'p20-t035-matrix.yml',
    'p20-t036-performance.yml', 'p20-t037-observability.yml',
)
JOB_AUTHORITY = {
    'p14-support-tickets-mail.yml': 'P14 contract freeze',
    'p15-authentication-oauth-account.yml': 'P15 contract guard and T028 coherence',
}
REPOSITORY = 'Techshrr/GoJet'


def validate_inventory(root):
    current={p.name for p in (root/'.github/workflows').glob('p[01][0-9]-*.yml')}
    require(current==set(REQUIRED_PREDECESSORS)|set(HISTORICAL),'undisposed predecessor workflow inventory drift')
    require(not set(REQUIRED_PREDECESSORS)&set(HISTORICAL),'ambiguous predecessor disposition')
    require(all((root/'.github/workflows'/name).is_file() for name in REQUIRED_P20),'missing P20 producer definition')
    return REQUIRED_PREDECESSORS+REQUIRED_P20


def select_latest(runs, workflow, head):
    eligible=[r for r in runs if r.get('path')=='.github/workflows/'+workflow
              and r.get('head_sha')==head and r.get('event') in ('push','pull_request','workflow_dispatch')
              and r.get('head_repository',{}).get('full_name')==REPOSITORY]
    return max(eligible,key=lambda r:(r['id'],r.get('run_attempt',1))) if eligible else None


def admit_run(run, jobs, workflow, head):
    require(run is not None and run.get('head_sha')==head and run.get('path')=='.github/workflows/'+workflow
            and run.get('head_repository',{}).get('full_name')==REPOSITORY,'missing or foreign regression authority')
    require(run.get('status')=='completed','regression authority still running')
    require(jobs and all(j.get('run_id')==run['id'] for j in jobs),'regression job/run identity mismatch')
    # Failed-job retries may reuse earlier successful jobs. For each logical job,
    # the newest execution is authoritative; an older green cannot hide a failure.
    latest={}
    for job in jobs:
        name=job['name']
        if name not in latest or job['id']>latest[name]['id']:latest[name]=job
    scoped=JOB_AUTHORITY.get(workflow)
    if scoped:
        require(scoped in latest,'required combined-workflow contract job missing')
        selected=[latest[scoped]]
    else:
        require(run.get('conclusion')=='success','required regression workflow failed')
        selected=list(latest.values())
    require(all(j.get('status')=='completed' and j.get('conclusion')=='success' for j in selected),'required regression job failed, skipped or cancelled')
    return {'workflow':workflow,'head_sha':head,'run_id':run['id'],'run_attempt':run.get('run_attempt',1),
            'jobs':[{'id':j['id'],'name':j['name'],'conclusion':j['conclusion']} for j in sorted(selected,key=lambda j:j['name'])],
            'admission_scope':scoped or 'complete workflow','status':'PASS'}
