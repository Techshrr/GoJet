from pathlib import Path
import unittest

from cross_release_test import inject_checkpoint


class InterruptionFixtureTests(unittest.TestCase):
    def test_only_phase_persistence_gets_kill_hook(self):
        code=(Path(__file__).resolve().parents[2]/'installer/src/NativeUpgrade.php').read_text()
        self.assertGreater(code.count("UpgradeIO::record($job.'/state.json',$state);"),1)
        changed=inject_checkpoint(code)
        start=code.index('    private static function phase(')
        end=code.index('    private static function waitHealthy(',start)
        self.assertEqual(changed[:start],code[:start])
        self.assertEqual(changed[changed.index('    private static function waitHealthy('):],code[end:])
        self.assertEqual(changed.count("getenv('P22_STOP_PHASE')"),1)
        with self.assertRaises(ValueError):
            inject_checkpoint(changed)

    def test_missing_or_ambiguous_phase_is_refused(self):
        code=(Path(__file__).resolve().parents[2]/'installer/src/NativeUpgrade.php').read_text()
        with self.assertRaises(ValueError):
            inject_checkpoint(code.replace('private static function phase(', 'private static function other('))
        hook="        UpgradeIO::record($job.'/state.json',$state);"
        with self.assertRaises(ValueError):
            inject_checkpoint(code.replace(hook,hook+'\n'+hook,1))


if __name__=='__main__':
    unittest.main()
