"""Compose static-only native routing from inherited fragments and actual payload bytes."""
import re

PREFIX = '/opt/gojet/current'


def render(files, site_fragment, docs_fragment):
    for app in ['site', 'docs', 'workspace', 'admin']:
        if not any(p.startswith('static/'+app+'/') for p in files):
            raise ValueError('Missing compiled app: '+app)
    for app in ['site', 'workspace', 'admin']:
        if 'static/'+app+'/index.html' not in files:
            raise ValueError('Missing compiled entry: '+app)
    assets = {}
    for app in ['site', 'workspace', 'admin']:
        prefix = 'static/'+app+'/'
        for path in sorted(files):
            if not path.startswith(prefix+'assets/'):
                continue
            url = '/'+path[len(prefix):]
            if not re.fullmatch(r'/assets/[A-Za-z0-9_./-]+', url) or '..' in url.split('/'):
                raise ValueError('Unsafe asset URL')
            if url in assets and files[assets[url]] != files[path]:
                raise ValueError('Conflicting compiled asset URL: '+url)
            assets.setdefault(url, path)
    # Every location gets its own root: Docs rewrites remove /docs/ before try_files.
    def rooted(fragment, app):
        return re.sub(r'(?m)^(location [^\n]+\{)\s*$',
                      lambda m: m[1]+'\n    root '+PREFIX+'/static/'+app+';', fragment)
    sections = ['# Generated from compiled payload. Include inside a TLS server block.\n'
                '# API/public dynamic routes and Installer are composed separately.\n',
                rooted(site_fragment, 'site'), rooted(docs_fragment, 'docs')]
    if 'static/site/favicon.ico' in files:
        sections.append('location = /favicon.ico {\n    alias '+PREFIX+'/static/site/favicon.ico;\n}\n')
    for app, route in [('workspace', 'app'), ('admin', 'admin')]:
        for selector in ['= /'+route, '^~ /'+route+'/']:
            sections.append('location '+selector+' {\n'
                            '    root '+PREFIX+'/static/'+app+';\n'
                            '    add_header Cache-Control "no-store" always;\n'
                            '    add_header X-Robots-Tag "noindex, nofollow" always;\n'
                            '    try_files /index.html =404;\n}\n')
    # Exact asset locations take precedence over the Website asset prefix/regex.
    # Identical shared bytes can reuse a file; differing bytes at one URL reject the build.
    for url, path in sorted(assets.items()):
        cache = 'public, max-age=31536000, immutable' if url.endswith(('.js', '.css')) else 'public, max-age=86400'
        sections.append('location = '+url+' {\n    alias '+PREFIX+'/'+path+';\n'
                        '    add_header Cache-Control "'+cache+'" always;\n}\n')
    return '\n'.join(sections).encode()
