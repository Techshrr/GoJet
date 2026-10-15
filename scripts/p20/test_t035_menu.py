"""Menu admission unit fixtures; not native browser evidence."""
import copy
import json
import tempfile
import unittest
from pathlib import Path
from t035_matrix import inspect_menu


class MenuAdmissionTest(unittest.TestCase):
    def test_raw_focus_and_interaction_claims_must_agree(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); rows = []
            for size in ('mobile', 'compact320'):
                for locale in ('en', 'zh-CN'):
                    row = dict(viewport=size, locale=locale, rapid_reopen_cycles=5, keyboard_controls=8)
                    row.update({key: True for key in ('enter_open','modal','forward_and_reverse_contained',
                        'escape_close','close_button','trigger_focus_returned','link_navigation')})
                    rows.append(row)
                    steps = [dict(key=key, index=i % 8, tag='A', inside=True, modal=True, document_has_focus=True)
                             for key in ('Tab', 'Shift+Tab') for i in range(16)]
                    trace = dict(implementation_commit='head', viewport=size, locale=locale, errors=[], steps=steps)
                    path = root / f'website/menu-traces/T025-menu-{size}-{locale}.json'
                    path.parent.mkdir(parents=True, exist_ok=True); path.write_text(json.dumps(trace))
            path = root / 'website/interactions/P19-T025.json'
            path.parent.mkdir(parents=True); data = {'details': {'mobileMenu': rows}}
            path.write_text(json.dumps(data)); self.assertEqual(inspect_menu(root, 'head')['raw_tab_steps'], 128)
            for field, value in [('escape_close', False), ('rapid_reopen_cycles', 0), ('keyboard_controls', 5)]:
                bad = copy.deepcopy(data); bad['details']['mobileMenu'][0][field] = value
                path.write_text(json.dumps(bad))
                with self.assertRaises(ValueError): inspect_menu(root, 'head')
            path.write_text(json.dumps(data))
            trace_path = root / 'website/menu-traces/T025-menu-mobile-en.json'
            source = json.loads(trace_path.read_text())
            for mutate in [lambda d: d.update(implementation_commit='other'), lambda d: d['steps'].pop(),
                           lambda d: d['steps'][0].update(inside=False),
                           lambda d: [s.update(index=0) for s in d['steps']]]:
                bad = copy.deepcopy(source); mutate(bad); trace_path.write_text(json.dumps(bad))
                with self.assertRaises(ValueError): inspect_menu(root, 'head')


if __name__ == '__main__': unittest.main()
