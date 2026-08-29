#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import urllib.parse
import xml.etree.ElementTree as ET
from html.parser import HTMLParser
from pathlib import Path
from typing import Any

from core_cases import API_ROUTES, API_SOURCE, CONTENT, DOCS, EVIDENCE_ROOT, MANIFEST_PATH, PUBLIC_BASE, canonical_links, html_facts, request, robots_noindex, source_path

ROOT = Path(__file__).resolve().parents[2]
DIST = DOCS / 'dist'
FIXED_SERVICES = ['redirectengine', 'analyticsworker', 'analyticsreconciler', 'platformapi', 'mailworker', 'fileworker', 'operationsmonitor', 'logreceiver']


class CrawlFacts(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.hrefs: list[str] = []
        self.srcs: list[str] = []
        self.heading_ids: list[str] = []
        self.text: list[str] = []
    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        values = dict(attrs)
        if tag == 'a' and values.get('href'): self.hrefs.append(values['href'] or '')
        if tag in ('img', 'script', 'source') and values.get('src'): self.srcs.append(values['src'] or '')
        if tag in ('h2', 'h3') and values.get('id'): self.heading_ids.append(values['id'] or '')
    def handle_data(self, data: str) -> None:
        if data.strip(): self.text.append(data.strip())


def manifest() -> dict[str, Any]:
    return json.loads(MANIFEST_PATH.read_text(encoding='utf-8'))


def git_head() -> str:
    return subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()


def write(case_id: str, payload: dict[str, Any]) -> None:
    plan = json.loads((EVIDENCE_ROOT / 'test-plan.json').read_text(encoding='utf-8'))
    row = next(item for item in plan['cases'] if item['id'] == case_id)
    path = ROOT / row['evidence']
    path.parent.mkdir(parents=True, exist_ok=True)
    data = {'case': case_id, 'status': 'PASS', 'implementation_commit': git_head(), 'secret_safe': True, **payload}
    path.write_text(json.dumps(data, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    print(json.dumps(data, indent=2, sort_keys=True))


def probe(locale: str, query: str, offline: bool = False) -> dict[str, Any]:
    env = dict(os.environ)
    raw = subprocess.check_output(['node', 'scripts/p18/pagefind_probe.mjs', '--locale', locale, '--query', query, '--offline', '1' if offline else '0'], cwd=ROOT, env=env, text=True)
    return json.loads(raw)


def t008(data: dict[str, Any]) -> dict[str, Any]:
    result = probe('en', 'Workspace API keys')
    assert result['state'] == 'results', result
    assert any('/docs/en/api/api-keys' in href for href in result['hrefs']), result
    assert all('/docs/zh-CN/' not in href and '/search' not in href for href in result['hrefs']), result
    assert result['external_requests'] == [], result
    eligible = sorted(entry['canonicalPath'] for entry in data['documents'] if entry['locale'] == 'en' and entry['indexable'] and entry['releaseState'] == 'published')
    return {'query': 'Workspace API keys', 'probe': result, 'eligible_published_english': eligible, 'pagefind_bundle': (DIST / 'pagefind/pagefind.js').is_file()}


def t009(data: dict[str, Any]) -> dict[str, Any]:
    result = probe('zh-CN', '原生自托管')
    assert result['state'] == 'results', result
    assert any('/docs/zh-CN/self-hosting' in href for href in result['hrefs']), result
    assert all('/docs/en/' not in href and '/search' not in href for href in result['hrefs']), result
    assert result['external_requests'] == [], result
    eligible = sorted(entry['canonicalPath'] for entry in data['documents'] if entry['locale'] == 'zh-CN' and entry['indexable'] and entry['releaseState'] == 'published')
    return {'query': '原生自托管', 'probe': result, 'eligible_published_zh_cn': eligible, 'locale_canonical_preserved': True}


def t010(data: dict[str, Any]) -> dict[str, Any]:
    routes = []
    for locale in ('en', 'zh-CN'):
        status, body, _ = request(f'/docs/{locale}/search?q=api')
        facts = html_facts(body)
        assert status == 200
        assert robots_noindex(facts), (locale, body[:500])
        assert canonical_links(facts) == [], canonical_links(facts)
        status_missing, _, _ = request(f'/docs/{locale}/search')
        assert status_missing == 400, (locale, status_missing)
        status_long, _, _ = request(f'/docs/{locale}/search?q={'x'*300}')
        assert status_long == 400, (locale, status_long)
        routes.append({'locale': locale, 'valid_status': status, 'missing_query_status': status_missing, 'oversize_query_status': status_long})
    empty = probe('en', 'qzxvjkbrmpfwy')
    assert empty['state'] == 'empty', empty
    offline = probe('en', 'API keys', True)
    assert offline['state'] == 'offline-static', offline
    for path in DIST.glob('sitemap*.xml'):
        assert '/docs/en/search' not in path.read_text(encoding='utf-8')
        assert '/docs/zh-CN/search' not in path.read_text(encoding='utf-8')
    return {'routes': routes, 'empty_state': empty, 'offline_state': offline, 'canonical_set_member': False, 'sitemap_member': False}


def parse_page(path: str) -> tuple[int, CrawlFacts, str]:
    status, body, _ = request(path)
    facts = CrawlFacts(); facts.feed(body)
    return status, facts, body


def t012(data: dict[str, Any]) -> dict[str, Any]:
    targets = ['/docs/en/api/api-keys', '/docs/zh-CN/api/webhooks', '/docs/en/self-hosting']
    rows = []
    forbidden = {entry['canonicalPath'] for entry in data['withdrawn']}
    for path in targets:
        status, facts, body = parse_page(path)
        assert status == 200
        assert facts.heading_ids, (path, facts.heading_ids)
        for heading in facts.heading_ids:
            assert f'#{heading}' in body, (path, heading)
        assert any('/docs/' in href and href != path for href in facts.hrefs), path
        assert all(not any(bad in href for bad in forbidden) for href in facts.hrefs)
        rows.append({'path': path, 'heading_ids': facts.heading_ids, 'internal_links': [href for href in facts.hrefs if '/docs/' in href][:20]})
    return {'articles': rows, 'heading_derived_toc': True, 'withdrawn_navigation_links': 0}


def normalized_internal(base_path: str, href: str) -> str | None:
    if not href or href.startswith(('#', 'mailto:', 'tel:', 'javascript:')): return None
    resolved = urllib.parse.urljoin(PUBLIC_BASE + base_path, href)
    parsed = urllib.parse.urlparse(resolved)
    if parsed.netloc != 'gojet.cc' or not parsed.path.startswith('/docs/'): return None
    path = parsed.path
    if path.endswith('/') and path not in ('/docs/en/', '/docs/zh-CN/'): path = path[:-1]
    return path


def t013(data: dict[str, Any]) -> dict[str, Any]:
    published = {entry['canonicalPath'] for entry in data['documents'] if entry['indexable'] and entry['releaseState'] == 'published'}
    graph: dict[str, set[str]] = {path: set() for path in published}
    incoming = {path: 0 for path in published}
    for path in sorted(published):
        status, facts, _ = parse_page(path)
        assert status == 200
        for href in facts.hrefs:
            target = normalized_internal(path, href)
            if target in published:
                graph[path].add(target)
                incoming[target] += 1
    reachable: set[str] = set()
    stack = ['/docs/en/', '/docs/zh-CN/']
    while stack:
        current = stack.pop()
        if current in reachable or current not in graph: continue
        reachable.add(current); stack.extend(sorted(graph[current] - reachable))
    orphans = sorted(path for path in published if path not in ('/docs/en/', '/docs/zh-CN/') and incoming[path] == 0)
    missing = sorted(published - reachable)
    assert not orphans, orphans
    assert not missing, missing
    return {'published_count': len(published), 'reachable_count': len(reachable), 'orphan_count': 0, 'incoming': incoming}


def sitemap_rows(path: Path) -> list[dict[str, str]]:
    root = ET.parse(path).getroot(); rows = []
    for child in root:
        loc = next((node.text for node in child if node.tag.endswith('loc')), None)
        lastmod = next((node.text for node in child if node.tag.endswith('lastmod')), None)
        if loc: rows.append({'loc': loc, 'lastmod': lastmod or ''})
    return rows


def t014(data: dict[str, Any]) -> dict[str, Any]:
    result = {}
    for locale in ('en', 'zh-CN'):
        path = DIST / f'sitemap-docs-{locale}.xml'
        assert path.is_file(), path
        rows = sitemap_rows(path)
        expected = sorted((PUBLIC_BASE + entry['canonicalPath'], entry['lastUpdated']) for entry in data['documents'] if entry['locale'] == locale and entry['indexable'] and entry['sitemap'] and entry['releaseState'] == 'published')
        actual = sorted((row['loc'], row['lastmod']) for row in rows)
        assert actual == expected, (locale, actual, expected)
        for loc, _ in actual:
            status, _, _ = request(urllib.parse.urlparse(loc).path)
            assert status == 200, (loc, status)
        result[locale] = rows
    return {'locale_sitemaps': result, 'content_owned_lastmod': True, 'build_time_lastmod': False}


def t015(data: dict[str, Any]) -> dict[str, Any]:
    broken = []; checked = set(); external = set()
    for entry in data['documents']:
        if not entry['indexable'] or entry['releaseState'] != 'published': continue
        status, facts, _ = parse_page(entry['canonicalPath']); assert status == 200
        for raw in facts.hrefs + facts.srcs:
            if not raw or raw.startswith(('#', 'data:', 'mailto:', 'tel:', 'javascript:')): continue
            resolved = urllib.parse.urljoin(PUBLIC_BASE + entry['canonicalPath'], raw)
            parsed = urllib.parse.urlparse(resolved)
            assert parsed.scheme in ('http', 'https')
            if parsed.netloc != 'gojet.cc':
                external.add(resolved); continue
            if not parsed.path.startswith('/docs/'): continue
            target = parsed.path
            if target in checked: continue
            checked.add(target)
            target_status, _, _ = request(target)
            if target_status != 200: broken.append({'source': entry['canonicalPath'], 'target': target, 'status': target_status})
    assert not broken, broken
    return {'internal_targets_checked': len(checked), 'broken': broken, 'external_syntax_checked': sorted(external), 'external_network_used_as_authority': False}


def code_blocks(text: str) -> list[tuple[str, str]]:
    return [(lang.strip(), body.strip()) for lang, body in re.findall(r'```([^\n]*)\n(.*?)\n```', text, flags=re.S)]


def t016(data: dict[str, Any]) -> dict[str, Any]:
    prohibited = ['docker run', 'docker compose', 'docker-compose', 'pm2 start', 'node server.js', 'node production runtime']
    checked = []
    known_routes = {route for routes in API_ROUTES.values() for route in routes}
    for entry in data['documents']:
        text = source_path(entry).read_text(encoding='utf-8')
        lowered = text.lower()
        hits = [token for token in prohibited if token in lowered]
        assert not hits, (entry['source'], hits)
        for lang, block in code_blocks(text):
            if lang == 'http':
                for line in block.splitlines(): assert line.strip() in known_routes, (entry['source'], line)
            if lang == 'text':
                assert len(block) < 4096
        checked.append({'source': entry['source'], 'code_blocks': len(code_blocks(text))})
    return {'documents': checked, 'prohibited_production_guidance': [], 'production_node_runtime_published': False, 'production_docker_compose_published': False}


def t017(data: dict[str, Any]) -> dict[str, Any]:
    pages = [source_path(entry).read_text(encoding='utf-8') for entry in data['documents'] if entry['canonicalPath'].endswith('/self-hosting')]
    assert len(pages) == 2
    joined = '\n'.join(pages)
    for service in FIXED_SERVICES: assert service in joined, service
    for token in ('Nginx', 'PHP', 'MySQL', 'Redis', 'ClamAV'):
        assert token in joined, token
    assert 'local storage' in pages[0].lower() and '本地存储' in pages[1]
    assert 'does **not** claim that the P21' in pages[0]
    assert '不宣称 P21' in pages[1]
    return {'fixed_services': FIXED_SERVICES, 'service_count': 8, 'architecture': ['Nginx', 'PHP installer boundary', 'systemd Go services', 'MySQL', 'Redis', 'ClamAV', 'local storage'], 'p21_complete_claim': False, 'p22_complete_claim': False}


def t018(data: dict[str, Any]) -> dict[str, Any]:
    route_source = API_SOURCE.read_text(encoding='utf-8')
    api_key_source = (ROOT / 'internal/admin/api_keys.go').read_text(encoding='utf-8')
    assert 'strings.Contains(scope, "*")' in api_key_source
    assert 'scope == requiredScope' in api_key_source
    checked = []
    dangerous = [re.compile(r'\bsk-[A-Za-z0-9_-]{16,}\b'), re.compile(r'\beyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]+\.'), re.compile(r'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----')]
    for entry in data['documents']:
        if entry['kind'] != 'api': continue
        text = source_path(entry).read_text(encoding='utf-8')
        for route in API_ROUTES[entry['capability']]:
            assert route in route_source and route in text, (entry['source'], route)
        assert all(not pattern.search(text) for pattern in dangerous), entry['source']
        if entry['capability'] == 'CAP-API-KEYS':
            assert 'Scope' in text or 'scope' in text
            assert '*' not in '\n'.join(block for lang, block in code_blocks(text) if lang == 'http')
            assert '<API_KEY_RETURNED_ONCE>' in text
        if entry['capability'] == 'CAP-USER-WEBHOOKS':
            assert 'WEBHOOK_ID' in text and 'DELIVERY_ID' in text
        checked.append({'source': entry['source'], 'capability': entry['capability'], 'routes': API_ROUTES[entry['capability']]})
    return {'documents': checked, 'scope_authority': {'wildcards_rejected': True, 'authorization_match': 'exact'}, 'real_secret_examples': 0, 'route_authority': 'current repository'}


CASES = {'P18-T008': t008, 'P18-T009': t009, 'P18-T010': t010, 'P18-T012': t012, 'P18-T013': t013, 'P18-T014': t014, 'P18-T015': t015, 'P18-T016': t016, 'P18-T017': t017, 'P18-T018': t018}


def main() -> int:
    parser = argparse.ArgumentParser(); parser.add_argument('--case', required=True, choices=sorted(CASES)); args = parser.parse_args()
    data = manifest()
    result = CASES[args.case](data)
    write(args.case, result)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
