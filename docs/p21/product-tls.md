# Product HTTPS configuration

`ProductWebConfig::render()` is a root-only renderer taking required origin,
release, certificate, private_key and max_body_bytes, with optional http_port. It reads the three packaged
static/platform/redirect fragments and produces a product HTTPS virtual host plus
a rejecting default TLS host. It does not write Nginx configuration, reload a
service, advance Installer state or claim installation completion.

The caller must first verify the approved release with PackageIntegrity. Release
and fragment paths must be canonical and root-controlled; fragments and TLS files
must be root-owned regular single-link files, not writable by other users. The
private key additionally forbids all group/other access. Fragments are bounded.
Only a lowercase HTTPS DNS origin without credentials/path/query/fragment is
accepted. IP-only origins are deliberately outside this initial configuration.
Paths reject directives, whitespace and traversal. Body limits are explicit
positive integers up to1GiB; the apply layer must align them with product quotas.

Unknown SNI fails the TLS handshake. Wrong Host or mismatched SNI/Host returns421;
canonical Host optionally permits explicit443 only on standard TLS port443. The
TLS versions are1.2/1.3. All three routing fragments share this server, preserving
static location priority and private proxy behavior. `/install` is closed in this
product configuration; the separately verified bootstrap vhost/FPM authority
remains separate until privileged apply can safely switch configurations.

The real compiled-payload fixture now uses a SAN certificate and verified TLS
for all page/asset/proxy requests. It tests wrong Host, unknown SNI server rejection,
body limit enforcement and unsafe render inputs/private-key modes. Only the SNI
negative probe disables client hostname comparison, so its failure must come from
the server rather than a client certificate-name error; trusted CA verification
remains enabled. Proxy upstreams are still explicitly labelled transport probes,
not real Go business proof. Full site/custom-domain certificates, host conflicts, real business/service health and safe root apply remain
pending before P21 closure. No production deployment is performed.

When http_port is supplied, it must be an integer TCP port distinct from the TLS
port. The renderer adds an HTTP default host rejecting unknown authorities with
421 and a named host returning308 to the configured HTTPS origin. Host must match
the configured HTTP port (bare hostname also permitted on80). The destination
never uses request Host or forwarding headers. Raw path and query are retained;
308 preserves the method on conforming clients. HTTP responses use no-store and
access logging is disabled. No request is proxied or served over cleartext.
Omitting http_port preserves the TLS-only configuration. The real Nginx fixture
checks six methods, encoded/duplicate query parameters, a double-slash path,
Installer path, forged forwarding headers and wrong Host/port. TLS certificate
issuance, HTTP ACME challenge handling and custom-domain provisioning remain the
responsibility of the later privileged installation integration.
