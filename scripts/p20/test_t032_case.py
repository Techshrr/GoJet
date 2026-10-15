import unittest
from t032_case import inspect_private, inspect_seo, equal_fields

HEAD = 'a' * 40


def private_fixture():
    rows = []
    for path, roles in [
        ('/app/settings/danger', ['owner', 'admin', 'member', 'viewer', 'anonymous']),
        ('/admin/operations/jobs', ['owner', 'admin', 'member', 'viewer', 'anonymous', 'limited-admin']),
        ('/admin/platform/mail-templates/mail-test', ['mail-admin']),
    ]:
        rows.extend({'path': path, 'role': role, 'robots': ['noindex,nofollow'], 'canonical': 0,
                     'alternates': 0, 'structuredData': 0} for role in roles)
    return {'implementation_commit': HEAD, 'production_session': True, 'mocked_api': False, 'rows': rows}


class SEOAdmissionTests(unittest.TestCase):
    def test_private_authenticated_and_denied_pages(self):
        self.assertEqual(inspect_private(private_fixture(), HEAD), 12)

    def test_missing_duplicate_and_indexable_private_pages(self):
        for mode in ['missing', 'duplicate', 'indexable', 'canonical', 'alternates', 'structuredData', 'mock', 'foreign-head']:
            with self.subTest(mode=mode):
                data = private_fixture()
                if mode == 'missing': data['rows'].pop()
                elif mode == 'duplicate': data['rows'][-1] = data['rows'][0]
                elif mode == 'indexable': data['rows'][0]['robots'] = ['index,follow']
                elif mode == 'mock': data['mocked_api'] = True
                elif mode == 'foreign-head': data['implementation_commit'] = 'b' * 40
                else: data['rows'][0][mode] = 1
                with self.assertRaises(ValueError): inspect_private(data, HEAD)

    def test_boolean_is_not_zero_count(self):
        with self.assertRaises(ValueError): equal_fields({'count': False}, {'count': 0}, 'count')

    def test_other_head_or_failed_seo_is_rejected(self):
        for changes in [{'implementation_commit': 'b' * 40}, {'status': 'FAIL'}, {'errors': ['failure']}, {'case': 'P19-T004'}]:
            with self.subTest(changes=changes):
                item = {'implementation_commit': HEAD, 'status': 'PASS', 'errors': [], 'case': 'P19-T003', **changes}
                with self.assertRaises(ValueError): inspect_seo({'website/site-core/P19-T003.json': item}, HEAD)


if __name__ == '__main__':
    unittest.main()
