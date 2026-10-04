"""Exercise the actual workflow discovery function with paginated API responses."""
import ast
import io
import json
from pathlib import Path
import textwrap
from types import SimpleNamespace
import unittest
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[2]


def discovery(open_url):
    source = (ROOT / '.github/workflows/p08-evidence.yml').read_text()
    start = source.index('          def fetch_runs():')
    end = source.index('\n          deadline', start)
    function = ast.parse(textwrap.dedent(source[start:end]))
    namespace = {'json': json, 'sha': 'exact-candidate', 'repository': 'owner/repo', 'token': 'fixture',
                 'urllib': SimpleNamespace(parse=urllib.parse, request=SimpleNamespace(
                     Request=urllib.request.Request, urlopen=open_url))}
    exec(compile(function, 'p08-workflow-discovery', 'exec'), namespace)
    return namespace['fetch_runs']


class P08DiscoveryTests(unittest.TestCase):
    def test_every_page_keeps_failed_and_pending_runs(self):
        for size in (0, 99, 100, 140, 200, 999):
            with self.subTest(size=size):
                rows = [{'id': n, 'status': 'queued' if n % 2 else 'completed',
                         'conclusion': None if n % 2 else 'failure'} for n in range(size)]
                pages = []
                def open_url(request, timeout):
                    query = urllib.parse.parse_qs(urllib.parse.urlsplit(request.full_url).query)
                    self.assertEqual(query['head_sha'], ['exact-candidate'])
                    self.assertEqual(query['event'], ['pull_request'])
                    self.assertEqual(query['per_page'], ['100'])
                    page = int(query['page'][0])
                    pages.append(page)
                    return io.StringIO(json.dumps({'workflow_runs': rows[(page-1)*100:page*100]}))
                self.assertEqual(discovery(open_url)(), rows)
                self.assertEqual(pages, list(range(1, size // 100 + 2)))

    def test_unavailable_or_incomplete_listing_never_becomes_success(self):
        for payload in ({}, {'workflow_runs': None}, {'workflow_runs': [{}] * 100}):
            with self.subTest(payload_type=type(payload.get('workflow_runs'))):
                with self.assertRaises((KeyError, RuntimeError)):
                    discovery(lambda request, timeout: io.StringIO(json.dumps(payload)))()
        def failed(request, timeout):
            if urllib.parse.parse_qs(urllib.parse.urlsplit(request.full_url).query)['page'] == ['1']:
                return io.StringIO(json.dumps({'workflow_runs': [{}] * 100}))
            raise OSError('API unavailable')
        with self.assertRaises(OSError):
            discovery(failed)()


if __name__ == '__main__':
    unittest.main()
