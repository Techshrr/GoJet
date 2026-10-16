# Native Installer web configuration

`NativeWebConfig::render()` generates a dedicated bootstrap Nginx TLS virtual
host and PHP 8.3 FPM pool. It is packaged PHP code, not a runtime dependency on
build tooling. The renderer does not write host configuration, start services,
issue certificates, or authorize installation completion.

Inputs are an exact HTTPS origin (DNS or IPv4; optional port, no trailing slash),
an immutable release directory, private state directory outside that release,
a bootstrap token SHA256 digest, existing certificate/private-key paths, and a
Unix socket path. Directive injection, whitespace/control characters, relative
paths and dot traversal are rejected. Root provisioning must additionally verify
canonical paths, ownership, permissions, certificate/key validity and conflicts
with existing virtual hosts. Do not install arbitrary caller-supplied output.

The dedicated `gojet-installer` account is separate from the eight service
processes. Root owns release/configuration files. The Installer account owns
its private 0700 state directory; it has no write access to releases or product
service configuration. A root-owned final installation lock independent of web
write authority remains a requirement for the privileged apply integration.
The FPM master starts as root only to drop workers to this dedicated account.
Nginx workers use `www-data`; only that group can access the 0660 FPM socket.
The socket parent must exist with suitable traversal permissions before start.

The pool clears inherited environment and receives only explicit Installer
configuration. PHP filesystem access is restricted to the release and private
state. Error display is disabled. Nginx accepts only the documented INSTALL
routes and always executes the fixed index.php; arbitrary PHP paths, private
files and product paths return404. Access logging is disabled for this bootstrap
host to avoid collecting token-bearing URLs. Request headers passed to FPM are
explicitly listed; HTTPS comes from the real TLS connection, not a forwarded
header. TLS1.2/1.3, bounded request size and connection/read timeouts are enforced.

The `P21 Native Installer Web` workflow uses native Nginx and PHP-FPM on Ubuntu
24.04. It generates a disposable certificate with an IP SAN and verifies that
certificate using a trusted test CA file (never disables certificate checks).
The actual production entrypoint is exercised through TLS and a Unix FPM socket.
The fixture verifies real unprivileged workers, session cookies, origin/body
limits, step restrictions, private file denial and all post-lock404 responses.
No certificate, private key, session cookie, bootstrap token or raw configuration
is uploaded as evidence. The test does not deploy to an external host.

This is the bootstrap Installer virtual host, not the final composed public
site/API/workspace/admin/proxy configuration. Interactive forms, full dependency
preflight, Admin bootstrap, privileged apply, service health, root-owned final
lock and upgrade/restore are still pending. Browser and full-product-vhost
verification remain false in this evidence.
