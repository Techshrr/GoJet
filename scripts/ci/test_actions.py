import unittest
import urllib.parse
from scripts.ci.actions import workflow_runs


class DiscoveryTests(unittest.TestCase):
    def test_all_pages_preserve_filters_and_verdicts(self):
        for event in (None, 'pull_request'):
            for size in (0, 99, 100, 149, 200, 999):
                with self.subTest(event=event, size=size):
                    rows = [{'id': i, 'status': 'queued' if i % 2 else 'completed',
                             'conclusion': None if i % 2 else 'failure'} for i in range(size)]
                    pages = []
                    def api(url):
                        q = urllib.parse.parse_qs(urllib.parse.urlsplit(url).query)
                        self.assertEqual(q['head_sha'], ['candidate'])
                        self.assertEqual(q.get('event'), None if event is None else [event])
                        self.assertEqual(q['per_page'], ['100'])
                        page = int(q['page'][0]); pages.append(page)
                        return {'workflow_runs': rows[(page - 1)*100:page*100]}
                    self.assertEqual(workflow_runs(api, 'owner/repo', 'candidate', event=event), rows)
                    self.assertEqual(pages, list(range(1, size // 100 + 2)))

    def test_incomplete_or_failed_api_is_not_missing_producer(self):
        for payload in ({}, {'workflow_runs': None}, {'workflow_runs': ['invalid']}, {'workflow_runs': [{}]*100}):
            with self.subTest(payload=type(payload.get('workflow_runs'))):
                with self.assertRaises((KeyError, RuntimeError)):
                    workflow_runs(lambda url: payload, 'owner/repo', 'candidate')
        def api(url):
            if urllib.parse.parse_qs(urllib.parse.urlsplit(url).query)['page'] == ['1']:
                return {'workflow_runs': [{}]*100}
            raise OSError('API unavailable')
        with self.assertRaises(OSError):
            workflow_runs(api, 'owner/repo', 'candidate')


if __name__ == '__main__':
    unittest.main()
