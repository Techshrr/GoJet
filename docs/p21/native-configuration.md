# Typed native service configuration

`php8.3 installer/bin/configure-services.php DESTINATION` reads at most16KiB of
private JSON on stdin, validates the initial single-site native profile and calls
the existing atomic root-only environment writer. It emits only creation status,
file count and the required Nginx body limit. Secrets are never printed. The
separate low-level create-environment command remains available for reviewed
operator configuration; the Installer integration should use the typed compiler.

The exact schema is `gojet.native-configuration.v1` with these required fields:

| Field | Required properties |
| --- | --- |
| origin | Canonical HTTPS DNS origin, optional explicit port; no path or credentials |
| database | port, name, user, password; native loopback MySQL |
| redis | port, db0–15, password; native loopback Redis |
| secrets | key_id plus six independent64-character lowercase hex keys: csrf, grant, oauth, admin, file_public, text_public |
| files | root, clamav_socket, max_upload_bytes, max_files, max_bytes, types |
| quotas | text, bio, qr; positive bounded integers |
| smtp | host, port, from, username, password; both credentials or neither |
| risk | name, endpoint, policy_version; HTTPS endpoint without embedded credentials/query/fragment |
| auth_rate | limit1–1000 and window_seconds1–86400; required authentication rate policy |
| turnstile_secret | Required nonempty verification secret for Support/Trust |

`files.types` maps lowercase extensions to lists of explicit MIME types, e.g.
`{"pdf":["application/pdf"],"png":["image/png"]}`. Quota bytes cannot be below the
upload limit. The returned max_body_bytes adds1MiB multipart allowance and remains
within ProductWebConfig's1GiB bound. Apply must pass this returned limit to that
renderer, not accept a second independent client value. Storage/socket paths are
syntactically checked here; root ownership, overlaps with release/Installer state,
actual storage access, scanner health and site/port conflicts require preflight
and privileged apply. This compiler does not claim those live checks.

A single database and Redis selection feeds all consumers. The generated API and
redirect listeners are fixed at127.0.0.1:8081/8080, matching the packaged Nginx
fragments. Only loopback proxies are trusted. Logs use the private packaged
logreceiver socket. Each service receives only the secrets/settings it needs;
logreceiver receives no database, SMTP or encryption credentials. Admin key ID/key
must be the same authority supplied to initial administrator bootstrap. Generate
keys once with a cryptographic RNG, retain them privately, and reuse them on
restart; this compiler does not silently rotate persisted authorities.

The initial profile enables existing core Auth/Account/Admin/Workspace/Billing/
Files/Text/Bio/Analytics/Support/Trust APIs. Payment providers, webhook delivery and
Admin service restart remain disabled until separately configured. OAuth and
Google One Tap continue using their existing database-owned Admin switches; no
provider is created or enabled by compilation. External credentials are operator
inputs, not generated test defaults. SMTP delivery, Turnstile verification and
risk-provider responses need their own live acceptance; compiling an endpoint
never proves provider health. Later provider configuration must preserve these
existing authorities rather than replacing them with Installer test adapters.

Native data CI validates rejection before file creation, consumes all eight exact
files through real PID1 as nobody, and starts the actual compiled platformapi with
its unmodified generated EnvironmentFile against native migrated MySQL/Redis.
It verifies all core API startup flags, public provider discovery and inherited
unauthenticated401/403 boundaries and a real Redis-backed login429 at the configured threshold. External-provider fixture values are explicitly
unused; there is no external delivery/risk/verification claim. This is configuration
integration; the separate payload fixture covers service health, sealed
web-to-root apply, the completion lock and browser progress. See
[current status](STATUS.md) for verified integration and remaining G11 gates.
The package remains non-installable.
