# Native service environment generation

The trusted root CLI `php8.3 installer/bin/create-environment.php DESTINATION`
reads a bounded JSON object from stdin. Its keys must be exactly the eight frozen
service names; each value is a map of GOJET environment variable names to UTF-8
strings. It produces the eight `.env` filenames referenced by the shipped units.
Pass credentials through private input, never command arguments or shell history.

Values preserve spaces, quotes, backslashes, dollar signs, backticks and percent
signs using systemd EnvironmentFile quoting. These files are not shell scripts and
must not be sourced. Multiline/control characters, non-GOJET variables, GOJET_TEST
and GOJET_P<number> test authorities are rejected. This validates serialization
and the native runtime boundary; it does not validate business feature semantics,
provider credentials or required settings for every enabled feature.

The canonical parent directory must be root-controlled. Generation serializes on
a root-private lock, writes a private temporary directory, fsyncs every file and
the directory, then renames the complete bundle into a previously absent target.
The bundle is0700 and each file0600, all root-owned. Existing targets are refused,
including symlinks; there is no overwrite or automatic upgrade. If interrupted,
an unpublished temporary directory may remain for root inspection. A failure
after rename must be inspected because the complete bundle may already exist.

This generates a reviewable configuration bundle. It does not install it into
`/etc/gojet`, start the eight product services, advance Installer state, or write
final completion authority. The later sealed apply step must approve and install
semantically validated settings. PID1 can read root-private EnvironmentFiles
before dropping to the `gojet` identity; the service account cannot read the files
directly. Native CI verifies this behavior with actual transient systemd probes
for all eight files, including special-character roundtrips and negative cases.
