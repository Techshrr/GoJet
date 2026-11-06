# Frontend Applications

GoJet V10 frontend applications are built in the pnpm workspace and shipped as static output where applicable. Node is not a production runtime.

Frozen application boundaries:

- `site` — Website + Auth
- `docs` — Astro Starlight static Docs
- `workspace` — customer Workspace
- `admin` — governance/Admin

The four applications are implemented in these directories. Their compiled static output is included in the native payload; installation acceptance is tracked in [P21 status](../../docs/p21/STATUS.md).
