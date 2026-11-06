# Frontend Packages

Frozen GoJet V10 shared package boundaries:

- `ui`
- `tokens`
- `api-client`
- `auth`
- `charts`
- `icons`
- `domain`
- `motion`
- `utils`

`tokens` is the authoritative generated token package. Business apps must consume shared tokens/UI/API-client contracts rather than creating private design systems or transport clients.

The shared implementations live in these packages. Generated assets consumed by multiple runtime surfaces must remain synchronized with their source contracts.
