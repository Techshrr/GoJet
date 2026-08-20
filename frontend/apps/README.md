# Frontend Applications

GoJet V10 frontend applications are built in the pnpm workspace and shipped as static output where applicable. Node is not a production runtime.

Frozen application boundaries:

- `site` — Website + Auth
- `docs` — Astro Starlight static Docs
- `workspace` — customer Workspace
- `admin` — governance/Admin

P00 freezes these boundaries. Application scaffolding and dependencies are introduced by the owning nodes after P00 passes.
