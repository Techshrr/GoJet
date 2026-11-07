# Google Web Risk integration 1.0.0

This auxiliary provider implements the existing GoJet semantic HTTPS contract.
It does not replace or modify the G11-approved `32ea29d` application archive,
installer state, database, eight application units, or existing port 80/443 sites.
It adds one external-dependency unit, not a ninth GoJet application process.

## Architecture and policy

`operationsmonitor -> https://SITE:9443/v1/check -> Nginx -> 127.0.0.1:9089 -> Google Lookup`

The current GoJet inspection client rejects loopback/private provider endpoints.
The site's public DNS must therefore resolve directly and exclusively to this
host's IPv4 address. The self-connection routes over `lo`; no firewall opening is
needed. Nginx binds only that address on 9443, checks the original TCP peer
(`$realip_remote_addr`), and denies every source except this server and 127.0.0.1.
This is not an anonymously accessible paid-API proxy. Do not add a public allow
rule, proxy it through Cloudflare, relax GoJet's SSRF guard or disable TLS checks.
The backend binds only 127.0.0.1:9089. Google egress uses IPv4 to match the key's
IP restriction. Any other local process can submit checks, as it already can
reach other local application dependencies; do not run untrusted local tenants.

Only `GET /v1/uris:search` is used. The key travels in `X-Goog-Api-Key`, never in a
URL. All three lists are requested: MALWARE, SOCIAL_ENGINEERING, UNWANTED_SOFTWARE.
Exact empty JSON `{}` becomes a provider `allow` observation (no known match,
not a guarantee of safety). A valid, nonexpired threat becomes `block`. Invalid,
partial, expired, oversized or non-JSON results, 401/403/429/5xx, TLS/network errors
and timeouts become HTTP 503 / incomplete `review`: the existing GoJet client
records `ProviderUnavailable`, and its local policy cannot allow that result.
The adapter never fetches the destination URL or follows upstream redirects.
It does not use the Update API or `threatLists.computeDiff`.

No API key, destination URL or raw Google error body is logged. Request bodies
are bounded; worker concurrency, sockets, memory and Nginx request rates are
bounded. There are no third-party Python dependencies or runtime package downloads.

## Install on the existing Ubuntu 24.04 test host

Upload/extract this directory outside `/opt/gojet/releases`. As root, from a real
interactive terminal (not a shell heredoc), run:

```bash
cd /root/GoJet-WebRisk-1.0.0
sha256sum --check SHA256SUMS
python3 -B install.py --host www.linketic.com
```

Enter the Google API key at the hidden prompt. It is stored root:root 0600 in
`/etc/gojet-webrisk/api-key`; systemd passes it using LoadCredential to DynamicUser.
Do not send the key in chat, put it in a command argument, or use shell tracing.
The script discovers the single DNS address and checks that it belongs to this
host (an explicit `--server-ip` remains available), local IP routing, ports, the existing site certificate and
Nginx before changes. Unknown existing paths stop installation. An installation
error removes only newly created owned configuration and preserves the key.
A repeat with identical code/config skips installation and reruns acceptance.
A failed live acceptance preserves the installed service and evidence for diagnosis.

Installed files:
- `/opt/gojet-integrations/google-webrisk/1.0.0/{adapter,verify}.py` and `gojet-webrisk-probe`
- `/etc/systemd/system/gojet-webrisk.service`
- `/etc/nginx/conf.d/gojet-webrisk.conf`
- `/etc/letsencrypt/renewal-hooks/deploy/90-gojet-webrisk-nginx`
- root-private key, file-hash receipt and timestamped acceptance JSON under
  `/etc/gojet-webrisk/`.

The Nginx listener reads the existing Let's Encrypt live certificate. The deploy
hook validates/reloads Nginx after renewal. This does not fix or change the main
application's ACME challenge routing or its separately pinned certificate copies;
those remain part of the later installation/renewal acceptance.

## Real acceptance (automatically run by install.py)

Five Google requests are made (including two through the original Go client): an example.com lookup, Google's official malware
test URL lookup, and a deliberately invalid test credential. The malware URL is
submitted as data, never visited. Results require no-match, block, and credential
rejection respectively. TLS certificate/hostname verification is always enabled.
Additional checks cover input rejection without upstream calls, Nginx rejection
of a non-allowlisted local source, wrong TLS hostname refusal, enabled/active unit
and a non-root service process. The included native probe runs as the existing
`gojet` user and uses the original SemanticProviderClient, SSRF-protected HTTP
client, and DestinationPolicy against the actual adapter endpoint. It requires
no database writes and verifies that the local safety gate remains enforced. A changing Google classification produces a
failed test, never a forged success.

Reports distinguish adapter acceptance from application installation. A healthz
response indicates only the local process, not Google availability. Live results
must be produced on the real host. Fixture tests are not live evidence. To rerun:

```bash
python3 -B install.py --host www.linketic.com
```

Only after the JSON says `"passed": true`, use these installer fields:

| Field | Value |
|---|---|
| Risk provider name | `google-web-risk` |
| Risk provider HTTPS endpoint | `https://www.linketic.com:9443/v1/check` |
| Risk policy version | `linketic-risk-v1` |

No Google key goes in the installer form. Keep the existing SMTP/Turnstile/DB
values. This integration does not claim G12/G13 or eight-service acceptance.
After application installation, verify a real scan job through operationsmonitor
and the persisted destination-risk decision; that remains a separate application
acceptance step requiring the initialized business database.

## Operation and rollback

Check `systemctl status gojet-webrisk` and the private acceptance reports. Google
401/403 suggests key/API/IP restrictions; 429 suggests quota; network errors may
be DNS/TLS/egress. Failure never becomes an allow observation. Rotate the key by
securely replacing the root-only key file and restarting this unit, then rerun
acceptance. Keep the Google project quota appropriate to expected traffic.

To remove this auxiliary integration, before saving the installer configuration:

```bash
python3 -B install.py --remove
```

Rollback checks every owned file hash before deletion, leaves the private key and
acceptance evidence, and removes only this integration's unit/Nginx config/hook
and version files. The built Go probe source is included as `probe-main.go`;
`BUILD.json` pins the repository commit used to build its imported GoJet code. It never clears a database or restores/replaces the installer's
site. After business use starts, removing it makes risk checks unavailable and
requires an intentional replacement provider first.

## Local tests and evidence

```bash
python3 -B -m unittest discover -v
```

Tests use controlled upstream responses and real local HTTP sockets. They cover
all threat types, errors and quotas, malformed/partial/oversized/duplicate JSON,
TLS transport errors, header-only secrets, target/input rejection, bounded
concurrency, deployment ownership, checksums and permissions. They do not claim
Google connectivity, native systemd/Nginx execution or whole-product verification.

References:
- https://docs.cloud.google.com/web-risk/docs/lookup-api
- https://docs.cloud.google.com/docs/authentication/api-keys-use
