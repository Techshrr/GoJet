# Native Deployment Boundary

GoJet V10 production deployment is native-only.

Reserved native configuration roots:

- `deploy/nginx/` — Nginx templates and validation assets
- `deploy/systemd/` — eight independent non-root Go service units

Production Dockerfiles, Compose files, `deploy/docker/`, PM2 and Node HTTP/SSR/dev-server runtime are prohibited.

Packaged native templates are consumed by the P21 preparation and activation commands. Current delivery limits are documented in [P21 status](../docs/p21/STATUS.md).
