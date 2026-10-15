"""Synthetic admission tests; never emitted as native visual evidence."""
import copy
import hashlib
import unittest
import tempfile
from pathlib import Path
from t034_matrix import inspect_surface, inspect_assets_and_states, inspect_files
from t028_sources import digest

class MatrixAdmissionTest(unittest.TestCase):
    def test_nested_manifests_are_checked_evidence(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'collection.json').write_text('{}')
            path = 'prerequisite/runtime/t033-sources/collection.json'
            nested = root / path
            nested.parent.mkdir(parents=True)
            nested.write_text('{"implementation_commit":"unit-fixture"}')
            files = {path: {'producer': 'prerequisite', 'sha256': digest(nested.read_bytes())}}
            inspect_files(root, files, set(files))
            nested.write_text('{"implementation_commit":"tampered"}')
            with self.assertRaisesRegex(ValueError, 'member digest mismatch'):
                inspect_files(root, files, set(files))
            nested.unlink()
            with self.assertRaisesRegex(ValueError, 'untracked raw evidence'):
                inspect_files(root, files, set(files))
            nested.write_text('{}')
            files[path]['sha256'] = digest(nested.read_bytes())
            extra = root / 'prerequisite/untracked/collection.json'
            extra.parent.mkdir(); extra.write_text('{}')
            with self.assertRaisesRegex(ValueError, 'untracked raw evidence'):
                inspect_files(root, files, set(files))

    def fixture(self):
        names = ['surface-canvas', 'surface-default', 'text-primary', 'text-secondary']
        css = ':root {' + ''.join('--gojet-' + n + ': #ffffff;' for n in names) + '}\n:root[data-theme="dark"] {' + ''.join('--gojet-' + n + ': #070b14;' for n in names) + '}'
        viewports = {'desktop': {'width': 1440, 'height': 900}, 'mobile': {'width': 390, 'height': 844}}
        data = {'implementation_commit': 'head', 'surface': 'docs', 'node': 'P18', 'status': 'PASS', 'errors': [], 'token_css_sha256': hashlib.sha256(css.encode()).hexdigest(), 'observations': []}
        captures = {}
        for size in viewports:
            for theme in ('light', 'dark'):
                name = f'docs-{size}-{theme}.png'; captures[name] = b'\x89PNG\r\n\x1a\nsynthetic unit fixture'
                data['observations'].append({'size': size, 'theme': theme, 'viewport': viewports[size], 'tokens': {'--gojet-' + n: '#fff' if theme == 'light' else '#070b14' for n in names}, 'reduced_motion': True, 'active_animations': 0, 'body': {'margin': '0px'}, 'browser_default_links': 0, 'overflow': False, 'broken_images': 0, 'placeholder_elements': 0, 'capture': name, 'capture_sha256': hashlib.sha256(captures[name]).hexdigest()})
        for row in data['observations']:
            row.update(images=[], notices=[], auth_state=None)
        return data, css, viewports, captures

    def test_native_notice_semantics(self):
        row = {'images': [], 'auth_state': 'invalid', 'notices': [{
            'tone': 'error', 'role': 'alert', 'has_text': True, 'focused': True,
            'foreground': 'rgb(0, 0, 0)', 'background': 'rgb(255, 255, 255)',
            'icon': {'classes': ['lucide', 'lucide-circle-alert'], 'hidden': 'true',
                     'view_box': '0 0 24 24', 'width': 16, 'height': 16, 'stroke': '1.75px'}}]}
        inspect_assets_and_states(row, 'auth-invalid')
        success = copy.deepcopy(row)
        success['auth_state'] = 'success'
        success['notices'][0].update(tone='success', role='status', focused=False)
        success['notices'][0]['icon']['classes'] = ['lucide', 'lucide-circle-check']
        inspect_assets_and_states(success, 'auth-verified')
        for field, value in [('icon', None), ('has_text', False), ('focused', False),
                             ('foreground', 'rgb(255, 255, 255)'), ('role', 'status')]:
            with self.subTest(field=field):
                bad = copy.deepcopy(row); bad['notices'][0][field] = value
                with self.assertRaises(ValueError): inspect_assets_and_states(bad, 'auth-invalid')
        with self.assertRaises(ValueError): inspect_assets_and_states(row, 'auth-verified')

    def test_native_image_rejection(self):
        image = dict(alt_present=True, width=20, height=20, natural_width=20, natural_height=20,
                     complete=True, same_origin=True, responsive=False, sizes=False, priority=None, loading=None)
        inspect_assets_and_states({'images': [image]}, 'website')
        for field, value in [('alt_present', False), ('width', 0), ('natural_height', 0),
                             ('same_origin', False), ('responsive', True)]:
            with self.subTest(field=field):
                bad = dict(image); bad[field] = value
                with self.assertRaises(ValueError): inspect_assets_and_states({'images': [bad]}, 'website')

    def test_valid_equivalent_color(self):
        data, css, viewports, captures = self.fixture()
        self.assertEqual(inspect_surface(data, 'docs', 'P18', 'head', css, viewports, captures), 4)

    def test_rejects_raw_failures_even_with_pass_claim(self):
        for key, value in [('active_animations', 1), ('browser_default_links', 1), ('overflow', True), ('broken_images', 1), ('placeholder_elements', 1), ('capture_sha256', 'wrong'), ('viewport', {'width': 1, 'height': 1})]:
            with self.subTest(key=key):
                data, css, viewports, captures = self.fixture(); data['observations'][0][key] = value
                with self.assertRaises(ValueError): inspect_surface(data, 'docs', 'P18', 'head', css, viewports, captures)

    def test_rejects_missing_matrix_and_foreign_head(self):
        data, css, viewports, captures = self.fixture()
        with self.assertRaises(ValueError): inspect_surface(data, 'docs', 'P18', 'other', css, viewports, captures)
        data['observations'][0] = copy.deepcopy(data['observations'][1])
        with self.assertRaises(ValueError): inspect_surface(data, 'docs', 'P18', 'head', css, viewports, captures)

if __name__ == '__main__': unittest.main()
