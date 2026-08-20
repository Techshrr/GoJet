# Native Deployment Boundary

GoJet V10 production deployment is native-only.

Reserved native configuration roots:

- `deploy/nginx/` — Nginx templates and validation assets
- `deploy/systemd/` — eight independent non-root Go service units

Production Dockerfiles, Compose files, `deploy/docker/`, PM2 and Node HTTP/SSR/dev-server runtime are prohibited.

P00 freezes this boundary. Native configuration implementation and release verification are delivered by later nodes and Gates.
