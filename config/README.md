# Configuration Boundary

This directory is reserved for non-secret GoJet V10 configuration schemas and examples.

Rules:

- Production secrets are never committed.
- Public/runtime settings must be explicitly classified and cannot expose privileged values.
- Environment-file contracts used by native systemd units are introduced with the owning deployment nodes.
- Example credentials must be unmistakably non-secret placeholders and may not resemble deployable production keys.
- No configuration may introduce a production Docker/Compose or Node-runtime path.
