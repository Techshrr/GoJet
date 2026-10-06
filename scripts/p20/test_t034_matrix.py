"""Synthetic admission tests; never emitted as native visual evidence."""
import copy
import hashlib
import unittest
from t034_matrix import inspect_surface

class MatrixAdmissionTest(unittest.TestCase):
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
        return data, css, viewports, captures

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
