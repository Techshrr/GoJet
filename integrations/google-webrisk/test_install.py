import hashlib
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch

import install


class InstallTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)

    def tearDown(self):
        self.directory.cleanup()

    def test_permissions_are_independent_of_root_umask(self):
        previous = os.umask(0o077)
        try:
            path = self.root / "adapter.py"
            install.write_new(path, b"code", 0o644)
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o644)
            secret = self.root / "api-key"
            install.write_new(secret, b"private", 0o600)
            self.assertEqual(stat.S_IMODE(secret.stat().st_mode), 0o600)
        finally:
            os.umask(previous)

    def test_existing_file_and_symlink_never_overwritten(self):
        path = self.root / "existing"
        path.write_bytes(b"keep")
        link = self.root / "link"
        link.symlink_to(path)
        for target in (path, link):
            with self.assertRaises(FileExistsError):
                install.write_new(target, b"replace")
        self.assertEqual(path.read_bytes(), b"keep")

    def test_owned_change_refuses_removal(self):
        file = self.root / "adapter.py"
        file.write_bytes(b"original")
        receipt = {"files": {str(file): install.sha(b"original")}}
        install.check_owned(receipt)
        file.write_bytes(b"changed")
        with self.assertRaisesRegex(RuntimeError, "changed"):
            install.check_owned(receipt)
        self.assertTrue(file.exists())

    def test_manifest_detects_missing_duplicate_and_changed_files(self):
        names = ("a", "b")
        for name in names:
            (self.root / name).write_bytes(name.encode())
        correct = "".join(install.sha(n.encode()) + "  " + n + "\n" for n in names)
        manifest = self.root / "SHA256SUMS"
        with patch.object(install, "HERE", self.root), patch.object(install, "SOURCE_FILES", names):
            manifest.write_text(correct)
            self.assertEqual(set(install.manifest()), set(names))
            for content in [correct.splitlines()[0] + "\n", correct + correct.splitlines()[0] + "\n"]:
                manifest.write_text(content)
                with self.assertRaises(RuntimeError):
                    install.manifest()
            manifest.write_text(correct)
            (self.root / "a").write_bytes(b"tampered")
            with self.assertRaisesRegex(RuntimeError, "checksum"):
                install.manifest()

    def test_nginx_boundaries(self):
        text = install.nginx_config("risk.example.com", "8.8.4.4").decode()
        self.assertIn("listen 8.8.4.4:9443 ssl;", text)
        self.assertIn("geo $realip_remote_addr", text)
        self.assertIn("deny all;", text)
        self.assertIn("if ($gojet_webrisk_local_peer = 0) { return 403; }", text)
        self.assertIn("proxy_pass http://127.0.0.1:9089;", text)
        self.assertIn("ssl_protocols TLSv1.2 TLSv1.3;", text)
        self.assertNotIn("listen 443", text)
        self.assertNotIn("listen 80", text)
        self.assertNotIn("api-key", text)
        self.assertIn("proxy_next_upstream off;", text)

    def test_dns_proxy_mismatch_stops_before_commands(self):
        with patch.object(install.socket, "getaddrinfo", return_value=[(2, 1, 6, "", ("8.8.8.8", 9443))]), patch.object(install, "run") as command:
            with self.assertRaisesRegex(RuntimeError, "DNS"):
                install.validate_host("risk.example.com", "8.8.4.4")
            command.assert_not_called()

    def test_hostname_injection_rejected(self):
        for host in ["foo;return 200;", "host/path", "HOST.test", "foo\nbar"]:
            with self.assertRaises(RuntimeError):
                install.validate_host(host, "8.8.4.4")

    def test_unit_has_private_credentials_and_no_root_or_public_binding(self):
        unit = (Path(__file__).parent / "gojet-webrisk.service").read_text()
        self.assertIn("DynamicUser=yes", unit)
        self.assertIn("LoadCredential=api-key:", unit)
        self.assertNotIn("Environment=", unit)
        self.assertIn("ProtectSystem=strict", unit)
        self.assertIn("MemoryMax=128M", unit)


if __name__ == "__main__":
    unittest.main(verbosity=2)
