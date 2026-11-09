FROM node:24.19.0-bookworm-slim AS frontend
WORKDIR /build
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig* ./
COPY frontend ./frontend
COPY scripts ./scripts
COPY artifacts/v10/P03 ./artifacts/v10/P03
RUN corepack enable && corepack prepare pnpm@11.21.0 --activate && pnpm install --frozen-lockfile
RUN pnpm run typecheck && pnpm run build

FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive PYTHONDONTWRITEBYTECODE=1
RUN printf '#!/bin/sh\nexit 101\n' > /usr/sbin/policy-rc.d && chmod +x /usr/sbin/policy-rc.d && \
    apt-get update && apt-get install -y --no-install-recommends ca-certificates openssl python3 supervisor nginx php8.3-cli php8.3-mysql mysql-server redis-server clamav clamav-daemon clamav-freshclam && \
    rm -rf /var/lib/apt/lists/* /var/lib/mysql/* /var/log/mysql/* && \
    useradd --system --no-create-home --shell /usr/sbin/nologin gojet && \
    useradd --system --no-create-home --shell /usr/sbin/nologin webrisk && usermod -a -G clamav gojet
COPY panel /opt/linketic
RUN python3 /opt/linketic/build_runtime.py && \
    php8.3 /opt/gojet/native/installer/bin/verify-package.php /opt/gojet/native ec4486c8852b51178d37e93addcaaa7cbb1b5755c4d3f8997757e32a85226f75 ce305a9207fd2fe46396e7cf433137c48ab5aec1
COPY --from=frontend /build/frontend/apps/site/dist /opt/gojet/current/static/site
COPY --from=frontend /build/frontend/apps/workspace/dist /opt/gojet/current/static/workspace
COPY --from=frontend /build/frontend/apps/admin/dist /opt/gojet/current/static/admin
COPY --from=frontend /build/frontend/apps/docs/dist /opt/gojet/current/static/docs
COPY scripts/p21/static_routes.py /build/scripts/p21/static_routes.py
COPY deploy/nginx /build/deploy/nginx
RUN python3 /opt/linketic/render_static.py && rm -rf /build /opt/linketic/tests /opt/linketic/build_runtime.py /opt/linketic/render_static.py && \
    printf 'Foreground yes\nLocalSocket /run/clamav/clamd.ctl\nLocalSocketMode 660\nLocalSocketGroup clamav\nFixStaleSocket yes\nUser clamav\nDatabaseDirectory /var/lib/clamav\nEnableVersionCommand yes\nLogTime yes\n' > /etc/clamav/clamd.conf && \
    printf 'DatabaseOwner clamav\nDatabaseDirectory /var/lib/clamav\nDatabaseMirror database.clamav.net\nChecks 12\n' > /etc/clamav/freshclam.conf
EXPOSE 8088
STOPSIGNAL SIGTERM
ENTRYPOINT ["python3","/opt/linketic/init.py"]
