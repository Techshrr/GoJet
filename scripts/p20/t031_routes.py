"""Registry binding for T031; static coverage never replaces native browser proof."""
import re
import subprocess
from pathlib import Path
from t028_sources import digest, require

ORACLE = '050fd7052d71ff77858b153abcbc466a1243af2f'
REGISTRY = 'specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md'
APPS = ('site', 'workspace', 'admin')


def normalize(path):
    path = path.split('?')[0].split('#')[0]
    return re.sub(r'\$\w+|\{[^}]+\}', '{}', path)


def approved_routes(source):
    routes = set()
    for line in source.splitlines():
        columns = line.split('|')
        if len(columns) < 5 or not re.search(r'`(?:APP|ADMIN|AUTH)-', columns[1]):
            continue
        if '`REQUIRED`' not in columns[4]:
            continue
        for route in re.findall(r'`(/[^`]+)`', columns[2]):
            if '[' in route:
                routes.add(normalize(re.sub(r'\[[^\]]+\]', '', route)))
            routes.add(normalize(route.replace('[', '').replace(']', '')))
    require(len(routes) == 88, 'frozen private/Auth registry inventory changed')
    return routes


def inspect_sources(registry, routers, shells):
    approved = approved_routes(registry)
    registered = set()
    for app, source in routers.items():
        # Only explicit route declarations are accepted for private/Auth paths.
        declarations = re.findall(r"const\s+(\w+)\s*=\s*createRoute\(\{[^\n]*?path:\s*['\"]([^'\"]+)['\"]", source)
        tree = source.split('rootRoute.addChildren(', 1)[-1].split(']);', 1)[0]
        for name, route in declarations:
            require(re.search(r'\b' + re.escape(name) + r'\b', tree), 'route omitted from tree: ' + route)
            registered.add(normalize(route))
    require(not approved - registered, 'missing registered routes: ' + ','.join(sorted(approved - registered)))
    private = {p for p in registered if p.startswith(('/app', '/admin', '/invite', '/oauth'))}
    require(not private - approved, 'invented private routes: ' + ','.join(sorted(private - approved)))
    links = set()
    for source in shells.values():
        for path in re.findall(r"['\"](/(?:app|admin)(?:/[^'\"]*)?)['\"]", source):
            target = normalize(path)
            require(target in approved and target in registered, 'unregistered shell destination: ' + path)
            links.add(target)
    require(links, 'missing shell links')
    return {'required_routes': sorted(approved), 'shell_destinations': sorted(links),
            'missing_routes': [], 'invented_private_routes': []}


def inspect(root):
    frozen = subprocess.check_output(['git', 'show', f'{ORACLE}:{REGISTRY}'], cwd=root)
    current = (root / REGISTRY).read_bytes()
    require(current == frozen, 'Route Registry differs from frozen oracle')
    paths = [f'frontend/apps/{app}/src/router.tsx' for app in APPS]
    shell_paths = [f'frontend/apps/{app}/src/shell/{app.title()}Shell.tsx' for app in ('workspace', 'admin')]
    sources = {path: (root / path).read_text() for path in paths + shell_paths}
    result = inspect_sources(current.decode(), {p: sources[p] for p in paths}, {p: sources[p] for p in shell_paths})
    return {**result, 'oracle_commit': ORACLE, 'registry_sha256': digest(current),
            'source_sha256': {p: digest(s.encode()) for p, s in sources.items()}}
