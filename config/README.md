# Configuration Boundary

This directory contains non-secret GoJet V10 configuration contracts.

Rules:

- Production secrets are never committed.
- Public/runtime settings must be explicitly classified and cannot expose privileged values.
- Environment-file contracts used by native systemd units are enforced by `installer/src/ServiceEnvironment.php` and the packaged unit templates.
- Example credentials must be unmistakably non-secret placeholders and may not resemble deployable production keys.
- No configuration may introduce a production Docker/Compose or Node-runtime path.
