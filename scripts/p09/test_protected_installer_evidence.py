"""Admission regressions using sanitized T024 details from real run 40373645402.

The historical fixture is unit-test input, never current-head acceptance evidence.
"""
import copy
import json
from pathlib import Path
import sys
import unittest

from protected_installer_evidence import validate_handoff

sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'p20'))
from t028_files import validate_handoff as validate_downstream

FIXTURE = Path(__file__).with_name('testdata')/'protected-installer-485487a.json'


def paths(value, prefix=()):
    for key, item in value.items():
        path = prefix+(key,)
        if isinstance(item, dict):
            yield from paths(item, path)
        else:
            yield path, item


def parent(value, path):
    for key in path[:-1]:
        value = value[key]
    return value


class ProtectedInstallerAdmission(unittest.TestCase):
    def setUp(self):
        self.good = json.loads(FIXTURE.read_text())

    def test_actual_protected_evidence_accepted_by_both_consumers(self):
        for validate in [validate_handoff, validate_downstream]:
            validate(copy.deepcopy(self.good))

    def test_every_required_observation_cannot_be_missing_or_changed(self):
        for path, original in paths(self.good):
            for mutation in ['missing', 'changed']:
                with self.subTest(path=path, mutation=mutation):
                    value = copy.deepcopy(self.good)
                    target = parent(value, path)
                    if mutation == 'missing':
                        del target[path[-1]]
                    else:
                        target[path[-1]] = not original if type(original) is bool else (
                            original+1 if type(original) is int else 'invalid')
                    for validate in [validate_handoff, validate_downstream]:
                        with self.assertRaises(ValueError):
                            validate(value)

    def test_malformed_containers_fail_closed(self):
        for path in [(), ('installer',), ('installer', 'viewports'),
                     ('installer', 'viewports', 'mobile'),
                     ('installer', 'viewports', 'desktop', 'scanner_down')]:
            for bad in [None, [], 'invalid', {}]:
                with self.subTest(path=path, value=bad):
                    value = copy.deepcopy(self.good)
                    if path:
                        parent(value, path)[path[-1]] = bad
                    else:
                        value = bad
                    with self.assertRaises(ValueError):
                        validate_handoff(value)

    def test_integer_boolean_substitution_rejected(self):
        for path, original in paths(self.good):
            if type(original) not in [int, bool]:
                continue
            with self.subTest(path=path):
                value = copy.deepcopy(self.good)
                parent(value, path)[path[-1]] = int(original) if type(original) is bool else False
                with self.assertRaises(ValueError):
                    validate_handoff(value)

    def test_legacy_public_step_pass_cannot_replace_protected_proof(self):
        value = copy.deepcopy(self.good)
        value['installer'] = {route: {'status': 200, 'state': 'step-pass'} for route in
            ['/install/environment', '/install/services', '/install/health']}
        value['fault_installer'] = {'status': 503, 'state': 'hard-failure'}
        with self.assertRaises(ValueError):
            validate_handoff(value)


if __name__ == '__main__':
    unittest.main()
