import tempfile
import unittest
from pathlib import Path
from catalog import catalog
class CatalogTests(unittest.TestCase):
    def test_original_bytes_and_failure_boundaries(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)
            with self.assertRaises(ValueError): catalog(p)
            (p/'000001_first.sql').write_text('CREATE TABLE test(id INT);\n')
            before=catalog(p)
            self.assertEqual(before['migrations'][0]['id'],1)
            (p/'000003_gap.sql').write_text('SELECT 1;')
            with self.assertRaises(ValueError): catalog(p)
            (p/'000003_gap.sql').unlink()
            (p/'000001_duplicate.sql').write_text('SELECT 1;')
            with self.assertRaises(ValueError): catalog(p)
            (p/'000001_duplicate.sql').unlink()
            (p/'000002_link.sql').symlink_to(p/'000001_first.sql')
            with self.assertRaises(ValueError): catalog(p)
            (p/'000002_link.sql').unlink()
            (p/'000001_first.sql').write_text('SELECT 2;')
            self.assertNotEqual(before['migrations'][0]['sha256'],catalog(p)['migrations'][0]['sha256'])
