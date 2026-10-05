"""T031 admission binds registry, predecessor and executed navigation authorities."""
import json
import subprocess
from pathlib import Path
from t028_sources import digest, require
from t031_sources import PRODUCERS
from t031_routes import inspect as inspect_routes

AUTH_STATES = {
    'login': {'input', 'invalid', 'success'}, 'register': {'input', 'code-sent'},
    'verify': {'verifying', 'success', 'reused-token'}, 'forgot': {'input', 'submitted-neutral'},
    'reset': {'input', 'success', 'invalid-token'}, 'oauth': {'processing', 'state-error'},
    'social': {'loading-handoff', 'expired-handoff'},
}


def inspect_navigation(sources, head):
    """Re-check observations, not just producer PASS declarations."""
    for path, value in sources.items():
        require(value.get('implementation_commit', value.get('exact_head')) == head,
                'navigation source head mismatch: ' + path)
        require(value.get('status') == 'PASS' and value.get('errors', []) == [], 'navigation source failed: ' + path)
        require(value.get('case', value.get('case_id')) == Path(path).stem, 'navigation case mismatch: ' + path)
    graph = sources['website/crawl/P19-T017.json']['details']
    count = graph['expected_per_locale']
    require(type(count) is int and count > 0 and graph['english_reachable'] == graph['zh_cn_reachable'] == count
            and graph['fake_locale_links'] == [], 'Website localization or reachability failure')
    edges = graph['graph']
    require(isinstance(edges, dict) and len(edges) == count * 2, 'incomplete Website graph')
    for locale, start in [('en', '/'), ('zh-CN', '/zh-CN/')]:
        nodes = {p for p in edges if p.startswith('/zh-CN') == (locale == 'zh-CN')}
        seen, pending = set(), [start]
        while pending:
            node = pending.pop()
            if node in seen:
                continue
            require(node in nodes, 'cross-locale or invented graph destination')
            seen.add(node)
            pending.extend(edges[node])
        require(seen == nodes and len(nodes) == count, 'unreachable Website route')
    links = sources['website/crawl/P19-T022.json']['details']
    require(links['navigation_edges_checked'] > 0 and links['assets_checked'] and links['published_docs_targets'],
            'empty Website link/asset proof')
    require(set(links['auth_targets']) == {'/login', '/register'}, 'Website Auth destinations changed')
    docs = sources['docs/navigation/P18-T012.json']
    require(docs['heading_derived_toc'] is True and docs['withdrawn_navigation_links'] == 0, 'invalid Docs navigation')
    require({a['path'] for a in docs['articles']} == {'/docs/en/api/api-keys', '/docs/zh-CN/api/webhooks', '/docs/en/self-hosting'}, 'incomplete Docs route samples')
    require(all(a['heading_ids'] and a['internal_links'] for a in docs['articles']), 'Docs TOC or links missing')
    docs_graph = sources['docs/crawl/P18-T013.json']
    require(docs_graph['published_count'] > 0 and docs_graph['published_count'] == docs_graph['reachable_count']
            and docs_graph['orphan_count'] == 0, 'Docs orphan routes')
    auth = sources['auth/browser/P15-T024.json']['details']
    require(auth['frozen_route_authority'] is True and auth['real_local_api'] is True, 'mocked Auth routes')
    for route, states in AUTH_STATES.items():
        require(states <= set(auth['states'].get(route, [])), 'missing Auth route states: ' + route)
    require(all(auth['security'].get(k) is True for k in ('noindex', 'private_headers', 'secure_cookie', 'web_storage_secret_free', 'raw_callback_not_rendered')), 'unsafe Auth navigation')
    admin = sources['admin/browser/P17-T031.json']
    for check in ('resource_inventory_routes', 'resource_link_deep_link', 'resource_routes_permission_denied',
                  'direct_route_permission_denied', 'fresh_mfa_for_high_risk'):
        require(admin['checks'].get(check) is True, 'missing Admin navigation check: ' + check)
    routes = admin['details']['resource_routes']
    require(len(routes) == 6 and {r['kind'] for r in routes} == {'links', 'domains', 'qr', 'text', 'bio', 'files'}, 'Admin inventory incomplete')
    for row in routes:
        target = '/admin/files' if row['kind'] == 'files' else '/admin/resources/' + row['kind']
        require(row['route'] == target and row['native_status'] == 200, 'Admin native route mismatch')
    require(any(r['kind'] == 'links' and r['count'] > 0 and r['detail_checked'] is True for r in routes), 'Admin seeded detail was not opened')
    notification = sources['notifications/p0/P20-T022.json']['details']
    for key in ('formal_p20_t022_claim', 'deep_link_authorized', 'real_session_authenticated', 'recipient_scoped', 'sensitive_data_redacted'):
        require(notification.get(key) is True, 'unsafe notification navigation: ' + key)
    require(notification['notification_deep_link'] == '/app/billing' and notification['list_http_status'] == 200, 'notification deep link mismatch')
    return {'website_pages': count * 2, 'docs_pages': docs_graph['published_count'],
            'auth_routes': sorted(AUTH_STATES), 'admin_resource_routes': routes,
            'notification_destination': '/app/billing'}


def inspect(root, head):
    directory = root / 'artifacts/v10/P20/runtime/t031-sources'
    manifest = json.loads((directory / 'collection.json').read_bytes())
    require(manifest.get('implementation_commit') == head, 'mixed-head T031 collection')
    require(set(manifest['artifacts']) == set(PRODUCERS), 'missing navigation producer')
    for key, artifact in manifest['artifacts'].items():
        require(artifact['head_sha'] == head and artifact['workflow'] == PRODUCERS[key][0]
                and artifact['run_id'] > 0 and artifact['artifact_id'] > 0
                and artifact['archive_sha256'].startswith('sha256:') and len(artifact['archive_sha256']) == 71, 'bad navigation provenance')
    files = manifest['files']
    require(set(files) == {str(p.relative_to(directory)) for p in directory.rglob('*') if p.is_file() and p != directory / 'collection.json'}, 'untracked navigation evidence')
    for path, record in files.items():
        require(not Path(path).is_absolute() and '..' not in Path(path).parts, 'unsafe navigation evidence path')
        require(record['producer'] == path.split('/')[0] and record['producer'] in PRODUCERS, 'navigation producer mismatch')
        require(digest((directory / path).read_bytes()) == record['sha256'], 'navigation source hash mismatch')
    from t030_case import inspect as inspect_t030
    prerequisite = directory / 'prerequisite'
    formal = json.loads((prerequisite / 'artifacts/v10/P20/consistency/P20-T030.json').read_bytes())
    require(formal.get('case') == 'P20-T030' and formal.get('status') == 'PASS' and formal.get('errors') == []
            and formal.get('implementation_commit') == head and formal['details'] == inspect_t030(prerequisite, head), 'invalid T030 prerequisite')
    sources = {}
    for key, (_, _, _, paths) in PRODUCERS.items():
        if key in ('prerequisite', 'workspace'):
            continue
        for path in paths:
            name = key + '/' + path
            require(name in files, 'unbound navigation evidence')
            sources[name] = json.loads((directory / name).read_bytes())
    from consistency_cases import inspect_browser
    browser = inspect_browser(directory / 'workspace', head)
    from t030_native import PACKAGE
    mail = directory / 'workspace/artifacts/v10/P20/runtime/t031/mail-template.jsonl'
    rows = [json.loads(line) for line in mail.read_bytes().splitlines()]
    require(rows and not any(row.get('Action') in ('fail', 'skip') for row in rows), 'mail template native test failed/skipped')
    for test in ('TestP20MailTemplateVersionAndAudit', None):
        require(sum(row.get('Action') == 'pass' and row.get('Package') == PACKAGE and row.get('Test') == test for row in rows) == 1, 'missing mail template native verdict')
    return {'formal_p20_t031_claim': True, 'next_case_unlocked': False,
            'prerequisite': {'case': 'P20-T030', 'head': head, 'revalidated': True},
            'registry': inspect_routes(root), 'navigation': inspect_navigation(sources, head),
            'workspace_browser': browser, 'mail_template_native_verified': True,
            'source_evidence': [{'path': path, 'sha256': files[path]['sha256']} for path in sorted(sources)],
            'collection_sha256': digest((directory / 'collection.json').read_bytes())}


def run_case():
    from common import ROOT, HEAD, emit
    details = {'formal_p20_t031_claim': False, 'next_case_unlocked': False}
    errors = []
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        errors.append('T031 admission failed: ' + str(error))
    return emit('P20-T031', 'consistency', 'Navigation route and deep-link consistency', errors, details)
