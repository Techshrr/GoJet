# Rainbow / auth.idcli.com runtime

Rainbow is optional and remains disabled until an administrator configures it.
It uses the documented HULINK `connect.php` login/callback protocol rather than
an OAuth token endpoint. This implementation supports one selected aggregate
channel at a time under the existing `rainbow` provider.

In Admin → Platform → OAuth → rainbow:

- Client ID: the service's `appid`.
- Client secret: its `appkey` (encrypted at rest; never returned to the browser).
- Authorization, token and user-info URLs: `https://auth.idcli.com/connect.php`.
- Redirect URI: the HTTPS GoJet site URL ending in `/oauth/rainbow/callback`.
- Scopes: exactly one of `qq`, `wx`, `alipay`, `baidu`, `huawei`, `google`,
  `facebook`, `twitter`, `dingtalk`, `gitee`, `github`.

The administrator save path validates the fixed endpoint and channel. Existing
invalid Rainbow configurations report incomplete. "Test provider" checks local
configuration readiness only; it does not prove upstream credentials or login.
LinkedIn is not a documented HULINK channel and is not accepted here.

The server calls `act=login` with credentials, the selected channel and a callback
URL containing GoJet state. Only the validated HTTPS authorization URL is returned
to the browser. Returned URLs with userinfo, appkey/client_secret parameters or
reflected secret values are rejected. HTTP redirects from the API are not followed.

The channel is included in the random state's prefix and authenticated by the
stored full-state hash. Callback exchange uses that channel; the current
configuration and upstream response must both match it. Changing the channel
invalidates outstanding login attempts. Replay is rejected before upstream I/O.
The identity namespace is `rainbow` plus channel plus `social_uid`; an aggregate
Google/QQ identity cannot alias the corresponding direct-provider identity.
No email verification is inferred from a Rainbow profile. Access tokens and
provider response bodies are not stored or exposed.

HULINK has no documented OAuth PKCE exchange: the local verifier remains encrypted
in the shared state store, but is not claimed as upstream PKCE enforcement.
Browser-state cookies and the existing one-time handoff/binding authorities remain
in force. Account connection now navigates to the returned authorization URL.

Tests cover protocol success/denial, redaction and durable state channel binding
with explicit upstream fixtures, plus built workspace navigation. Evidence is
`runtime/t025/rainbow-protocol-state.jsonl` and `oauth-connect-browser.json`.
A real configured application and consenting login are still required for live
provider evidence; these tests do not close T025 or unlock T026.
