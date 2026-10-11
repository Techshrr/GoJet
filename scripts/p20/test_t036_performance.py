"""Synthetic rejection fixtures only; never uploaded as native product evidence."""
import copy
import unittest
import tempfile
from pathlib import Path
from performance_cases import inspect_website, inspect_analytics, ANALYTICS_BUDGET
from t036_guards import verify, GUARDS

HEAD = 'a' * 40


def record(case, details):
    return {'case': case, 'status': 'PASS', 'implementation_commit': HEAD,
            'errors': [], 'details': details}


def website():
    bundle = record('P19-T027', {
        'budgetBytes': 153600, 'initialGzipBytes': 119981,
        'initialFiles': [{'file': 'assets/index-abc.js', 'rawBytes': 383730, 'gzipBytes': 119981}],
        'dynamicChunks': ['assets/ContactPage-abc.js'],
        'loadedScriptUrls': ['http://127.0.0.1:4194/assets/index-abc.js'], 'forbiddenBundleRequests': [],
    })
    cwv = record('P19-T028', {
        'labProfile': {'viewport': '1440x900', 'latencyMs': 40, 'downloadMbps': 10,
                       'uploadMbps': 5, 'browser': '/usr/bin/google-chrome'},
        'lcpMs': 244, 'inpMs': 16, 'cls': 0,
        'eventEntries': [{'entryType': 'first-input', 'interactionId': 1, 'duration': 16}],
        'renderedPrincipalImageCount': 0, 'fontResources': [],
        'nativePerformance': {
            'supportedEntryTypes': ['longtask', 'largest-contentful-paint', 'layout-shift', 'event', 'first-input'],
            'longTasks': [], 'observationStartMs': 0, 'observedUntilMs': 1500,
            'navigation': [{'startTime': 0, 'domContentLoadedEventEnd': 90, 'loadEventEnd': 100}],
            'renderedImages': [], 'fontFaces': [], 'resources': [],
        },
        'cache': {'hashedAsset': 'assets/index-abc.js', 'hashedCache': 'public, max-age=31536000, immutable',
                  'socialCard': '/assets/social/gojet-en.png', 'socialCache': 'public, max-age=86400'},
    })
    return bundle, cwv


class PerformanceRejection(unittest.TestCase):
    def test_canonical_guard_cannot_be_shortened_or_changed(self):
        root = Path(__file__).resolve().parents[2]
        self.assertEqual(set(verify(root)), set(GUARDS))
        with tempfile.TemporaryDirectory() as temporary:
            scratch = Path(temporary); directory = scratch / '.github/workflows'; directory.mkdir(parents=True)
            for workflow, _ in GUARDS.values():
                (directory/workflow).write_bytes((root/'.github/workflows'/workflow).read_bytes())
            target = directory/'p20-t036-performance.yml'
            original = (root/'.github/workflows/p20-t036-performance.yml').read_text()
            for altered in (original.replace('go test ./...', 'echo skipped'),
                            original.replace('pnpm -r --if-present typecheck', 'true'),
                            original.replace('  # END CANONICAL candidate\n', '')):
                target.write_text(altered)
                with self.assertRaises(ValueError): verify(scratch)

    def test_valid_profile_and_exact_budget_boundary(self):
        b, c = website()
        self.assertEqual(inspect_website(b, c, HEAD)['initial_js_gzip_bytes'], 119981)
        b['details']['initialGzipBytes'] = b['details']['initialFiles'][0]['gzipBytes'] = 153600
        c['details'].update(lcpMs=2500, inpMs=200, cls=.1)
        c['details']['eventEntries'][0]['duration'] = 200
        inspect_website(b, c, HEAD)

    def test_missing_raw_trace_is_not_grandfathered(self):
        b, c = website()
        del c['details']['nativePerformance']
        with self.assertRaises(KeyError): inspect_website(b, c, HEAD)

    def test_rejects_false_green_records(self):
        mutations = {
            'stale head': lambda b, c: c.update(implementation_commit='b' * 40),
            'failed native': lambda b, c: c.update(status='FAIL'),
            'raised gzip budget': lambda b, c: b['details'].update(budgetBytes=999999),
            'over budget': lambda b, c: b['details'].update(initialGzipBytes=153601),
            'false gzip total': lambda b, c: b['details']['initialFiles'][0].update(gzipBytes=1),
            'no dynamic split': lambda b, c: b['details'].update(dynamicChunks=[]),
            'hidden product script': lambda b, c: b['details']['loadedScriptUrls'].append('http://127.0.0.1:4194/assets/workspace-abc.js'),
            'missing scripts': lambda b, c: b['details'].update(loadedScriptUrls=[]),
            'faster lab profile': lambda b, c: c['details']['labProfile'].update(latencyMs=0),
            'bad lcp': lambda b, c: c['details'].update(lcpMs=2501),
            'NaN cls': lambda b, c: c['details'].update(cls=float('nan')),
            'boolean lcp': lambda b, c: c['details'].update(lcpMs=True),
            'no trusted event': lambda b, c: c['details'].update(eventEntries=[]),
            'invented inp': lambda b, c: c['details'].update(inpMs=0),
            'long native task': lambda b, c: c['details']['nativePerformance'].update(longTasks=[{'duration': 50}]),
            'unsupported observer': lambda b, c: c['details']['nativePerformance'].update(supportedEntryTypes=['event']),
            'excluded startup': lambda b, c: c['details']['nativePerformance'].update(observationStartMs=100),
            'incomplete window': lambda b, c: c['details']['nativePerformance'].update(observedUntilMs=50),
            'missing image': lambda b, c: c['details'].update(renderedPrincipalImageCount=1),
            'unreviewed font face': lambda b, c: c['details']['nativePerformance'].update(fontFaces=[{'display': 'block'}]),
            'hidden remote font': lambda b, c: c['details']['nativePerformance']['resources'].append({'name': 'https://other.invalid/font.woff2', 'initiatorType': 'css'}),
            'bad immutable cache': lambda b, c: c['details']['cache'].update(hashedCache='public, max-age=86400'),
            'stable asset immutable': lambda b, c: c['details']['cache'].update(socialCache='max-age=86400, immutable'),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                b, c = website(); mutate(b, c)
                with self.assertRaises((ValueError, KeyError, TypeError)): inspect_website(b, c, HEAD)

    def test_image_failure_cannot_hide_behind_count(self):
        b, c = website()
        image = {'src': 'http://127.0.0.1:4194/image.avif', 'complete': True,
                 'widthAttribute': '640', 'heightAttribute': '480', 'naturalWidth': 640, 'naturalHeight': 480,
                 'pictureSources': [{'type': kind, 'srcset': '/img 640w', 'sizes': '100vw'}
                                    for kind in ('image/avif', 'image/webp')]}
        c['details']['renderedPrincipalImageCount'] = 1
        c['details']['nativePerformance']['renderedImages'] = [image]
        inspect_website(b, c, HEAD)
        for field, value in [('widthAttribute', '0'), ('complete', False), ('pictureSources', []),
                             ('src', 'https://other.invalid/img.avif')]:
            with self.subTest(field=field):
                altered = copy.deepcopy(c)
                altered['details']['nativePerformance']['renderedImages'][0][field] = value
                with self.assertRaises(ValueError): inspect_website(b, altered, HEAD)

    def test_analytics_native_budget_and_gate_agreement(self):
        observed = {'dom_content_loaded_ms': 86, 'load_event_ms': 87, 'resource_bytes': 144058, 'resource_count': 12}
        case = record('P07-T019', {'performance': observed, 'performance_budget': ANALYTICS_BUDGET,
            'diagnostics': {k: [] for k in ('console_errors', 'page_errors', 'http_errors', 'request_failures')}})
        gate = {'status': 'PASS', 'implementation_commit': HEAD, 'observed': observed,
                'budget': ANALYTICS_BUDGET, 'evidence': {'no_client_generated_totals': True}}
        inspect_analytics(case, gate, HEAD)
        for key, limit in ANALYTICS_BUDGET.items():
            bad = copy.deepcopy(case); bad['details']['performance'][key] = limit + 1
            with self.assertRaises(ValueError): inspect_analytics(bad, gate, HEAD)
        bad_gate = copy.deepcopy(gate); bad_gate['observed']['resource_count'] = 1
        with self.assertRaises(ValueError): inspect_analytics(case, bad_gate, HEAD)


if __name__ == '__main__': unittest.main()
