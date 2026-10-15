import unittest
from pathlib import Path
from t031_case import AUTH_STATES, inspect_navigation
from t031_routes import REGISTRY, APPS, inspect_sources

HEAD = 'a' * 40
ROOT = Path(__file__).resolve().parents[2]


def fixture():
    # Synthetic admission fixtures, never emitted as product evidence.
    out = {}
    def add(path, details=None, **fields):
        out[path] = {'case': Path(path).stem, 'implementation_commit': HEAD, 'status': 'PASS', 'errors': [], **fields}
        if details is not None:
            out[path]['details'] = details
    add('website/crawl/P19-T017.json', {'expected_per_locale': 1, 'english_reachable': 1, 'zh_cn_reachable': 1,
        'fake_locale_links': [], 'graph': {'/': ['/'], '/zh-CN/': ['/zh-CN/']}})
    add('website/crawl/P19-T022.json', {'navigation_edges_checked': 2, 'assets_checked': ['/asset.css'],
        'published_docs_targets': ['/docs/en/'], 'auth_targets': ['/login', '/register']})
    add('docs/navigation/P18-T012.json', heading_derived_toc=True, withdrawn_navigation_links=0,
        articles=[{'path': p, 'heading_ids': ['intro'], 'internal_links': ['/docs/en/']}
                  for p in ['/docs/en/api/api-keys', '/docs/zh-CN/api/webhooks', '/docs/en/self-hosting']])
    add('docs/crawl/P18-T013.json', published_count=2, reachable_count=2, orphan_count=0)
    add('auth/browser/P15-T024.json', {'frozen_route_authority': True, 'real_local_api': True,
        'states': {k: sorted(v) for k, v in AUTH_STATES.items()}, 'security': {k: True for k in
        ['noindex', 'private_headers', 'secure_cookie', 'web_storage_secret_free', 'raw_callback_not_rendered']}})
    routes = [{'kind': k, 'route': '/admin/files' if k == 'files' else '/admin/resources/' + k,
               'native_status': 200, 'count': int(k == 'links'), 'detail_checked': k == 'links'}
              for k in ['links', 'domains', 'qr', 'text', 'bio', 'files']]
    add('admin/browser/P17-T031.json', {'resource_routes': routes}, checks={k: True for k in
        ['resource_inventory_routes', 'resource_link_deep_link', 'resource_routes_permission_denied',
         'direct_route_permission_denied', 'fresh_mfa_for_high_risk']})
    add('notifications/p0/P20-T022.json', {'formal_p20_t022_claim': True, 'deep_link_authorized': True,
        'real_session_authenticated': True, 'recipient_scoped': True, 'sensitive_data_redacted': True,
        'notification_deep_link': '/app/billing', 'list_http_status': 200})
    return out


class NavigationAdmissionTests(unittest.TestCase):
    def test_complete_fixture(self):
        self.assertEqual(inspect_navigation(fixture(), HEAD)['notification_destination'], '/app/billing')

    def test_rejects_unsafe_or_incomplete_sources(self):
        mutations = [
            ('auth/browser/P15-T024.json', ['implementation_commit'], 'b' * 40),
            ('auth/browser/P15-T024.json', ['details', 'real_local_api'], False),
            ('auth/browser/P15-T024.json', ['details', 'states', 'oauth'], ['processing']),
            ('website/crawl/P19-T017.json', ['details', 'graph', '/'], ['/zh-CN/']),
            ('website/crawl/P19-T017.json', ['details', 'fake_locale_links'], ['/zh-CN/invented']),
            ('docs/crawl/P18-T013.json', ['orphan_count'], 1),
            ('docs/navigation/P18-T012.json', ['withdrawn_navigation_links'], 1),
            ('admin/browser/P17-T031.json', ['checks', 'resource_routes_permission_denied'], False),
            ('notifications/p0/P20-T022.json', ['details', 'notification_deep_link'], '/admin/mail'),
            ('notifications/p0/P20-T022.json', ['details', 'recipient_scoped'], False),
        ]
        for path, keys, value in mutations:
            with self.subTest(path=path, keys=keys):
                data = fixture()
                target = data[path]
                for key in keys[:-1]:
                    target = target[key]
                target[keys[-1]] = value
                with self.assertRaises(ValueError):
                    inspect_navigation(data, HEAD)


class RegistryTests(unittest.TestCase):
    def setUp(self):
        self.registry = (ROOT / REGISTRY).read_text()
        self.routers = {app: (ROOT / f'frontend/apps/{app}/src/router.tsx').read_text() for app in APPS}
        self.shells = {'admin': (ROOT / 'frontend/apps/admin/src/shell/AdminShell.tsx').read_text()}

    def test_registered_routes(self):
        self.assertEqual(len(inspect_sources(self.registry, self.routers, self.shells)['required_routes']), 88)

    def test_missing_required_page(self):
        self.routers['workspace'] = self.routers['workspace'].replace("path: '/app/settings/danger'", "path: '/app/settings/invented'")
        with self.assertRaises(ValueError):
            inspect_sources(self.registry, self.routers, self.shells)

    def test_dead_sidebar(self):
        self.shells['admin'] += "const stale = '/admin/resources';"
        with self.assertRaises(ValueError):
            inspect_sources(self.registry, self.routers, self.shells)

    def test_unattached_route(self):
        self.routers['admin'] += "\nconst phantom = createRoute({ path: '/admin/phantom' });"
        with self.assertRaises(ValueError):
            inspect_sources(self.registry, self.routers, self.shells)


if __name__ == '__main__':
    unittest.main()
