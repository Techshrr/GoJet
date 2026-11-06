from pathlib import Path
import unittest
from archive import validate_php_dependencies


class PHPPayloadTests(unittest.TestCase):
    def test_real_installer_transitive_dependencies_are_required(self):
        root = Path(__file__).resolve().parents[2]
        files = {p.relative_to(root).as_posix(): p.read_bytes() for p in (root/'installer').rglob('*.php')}
        validate_php_dependencies(files)
        files.pop('installer/src/HostPreflight.php')
        with self.assertRaisesRegex(ValueError, 'HostPreflight.php'):
            validate_php_dependencies(files)

    def test_dynamic_or_escaping_include_is_not_a_package_dependency(self):
        for expression in ["getenv('HELPER')", "__DIR__.'/../../outside.php'"]:
            with self.subTest(expression=expression), self.assertRaises(ValueError):
                validate_php_dependencies({'installer/src/Example.php': ('<?php\nrequire_once '+expression+';').encode()})
