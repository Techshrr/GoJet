"""Prevent an evidence collector from waiting for workflows excluded by PR paths."""
import fnmatch
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]
COHORT = ('p16-evidence', 'p16-trust-destination-risk-abuse',
          'p16-notification-producer', 'p16-admin-risk-api', 'p16-browser',
          'p16-destination-risk-integration')


def patterns(name):
    text = (ROOT / '.github/workflows' / (name + '.yml')).read_text()
    block = text.split('    paths:\n', 1)[1].split('  workflow_dispatch:', 1)[0]
    return set(re.findall(r"      - '([^']+)'", block))


class CohortTests(unittest.TestCase):
    def test_collector_and_every_required_producer_have_identical_triggers(self):
        expected = patterns(COHORT[0])
        self.assertTrue(expected)
        for name in COHORT[1:]:
            self.assertEqual(expected, patterns(name), name)

    def test_release_entrypoint_and_cohort_edits_schedule_all_producers(self):
        # The release identity change triggered evidence/browser/integration, but
        # previously excluded contract and notification producers indefinitely.
        changed = ['services/platformapi/cmd/server/main.go',
                   'services/redirectengine/cmd/server/main.go',
                   'scripts/ci/test_p16_triggers.py']
        changed += ['.github/workflows/' + name + '.yml' for name in COHORT]
        for name in COHORT:
            for path in changed:
                self.assertTrue(any(fnmatch.fnmatchcase(path, p) for p in patterns(name)), (name, path))


if __name__ == '__main__':
    unittest.main()
