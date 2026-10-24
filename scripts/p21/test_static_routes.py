import unittest
from static_routes import render


class StaticRoutesTests(unittest.TestCase):
    def test_shared_url_requires_identical_bytes(self):
        files = {'static/'+app+'/index.html': b'page' for app in ['site', 'docs', 'workspace', 'admin']}
        files['static/workspace/assets/shared.js'] = b'workspace'
        files['static/admin/assets/shared.js'] = b'admin'
        with self.assertRaisesRegex(ValueError, 'Conflicting'):
            render(files, '', '')
        files['static/admin/assets/shared.js'] = b'workspace'
        self.assertEqual(render(files, '', '').count(b'location = /assets/shared.js'), 1)
        files['static/admin/assets/bad;directive.js'] = b'x'
        with self.assertRaisesRegex(ValueError, 'Unsafe'):
            render(files, '', '')
