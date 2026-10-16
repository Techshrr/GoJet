# Target-host package integrity preflight

The PHP 8.3 CLI verifier adds a read-only check of an extracted native release:

```sh
php8.3 /trusted/installer/bin/verify-package.php /absolute/release MANIFEST_SHA256 SOURCE_SHA
```

Run a trusted copy of the verifier as root. The expected manifest digest and source
must come from the independently approved release evidence, not a file downloaded
alongside an untrusted archive. Verify the archive digest before extraction into a
new root-owned directory. Do not execute code from an unverified extraction.

The verifier checks the root hierarchy, manifest identity, exact file inventory,
all member hashes/sizes/modes, SBOM hash and complete SHA256SUMS inventory. It
rejects missing or extra files/directories, symlinks, hardlinks, non-root ownership,
group/world-writable directories and unsafe member modes. It limits manifest,
member and total sizes. Release ancestors must remain root controlled; this does
not defend against another privileged actor modifying files concurrently.

A successful integrity result preserves `installable=false`: integrity does not
imply dependencies, configuration, services or health have passed. This is the
package-integrity portion of P21-T010. Integrating the aggregate mandatory
preflight with web-to-root apply remains outstanding. There is no final-lock or
service mutation in this verifier.

Payload CI extracts the actual compiled binary/static/migration/Installer archive
and invokes this same PHP code. It tests corruption, omission, extra files and
directories, wrong authority, ownership/mode changes, symlinks, hardlinks and
checksum inventory alteration, then verifies the restored release again. No
synthetic package substitutes for the native payload.
