#!/usr/bin/env python3
from integration_common import *


def assert_noindex_headers(headers, label):
    lower = headers_lower(headers)
    expect(lower.get("x-robots-tag", "").lower() == "noindex, nofollow", f"{label} X-Robots-Tag={lower.get('x-robots-tag')}")
    expect("no-store" in lower.get("cache-control", "").lower(), f"{label} Cache-Control={lower.get('cache-control')}")


def case_t013():
    available = create_share("p10-t013-available", visibility="public", content="seo-available")
    gated = create_share("p10-t013-gated", visibility="public", content="seo-gated", password="p10-seo-password")
    private = create_share("p10-t013-private", visibility="private", content="seo-private")
    expired = create_share("p10-t013-expired", visibility="public", content="seo-expired", expires_at=past_time())
    checks = []
    for label, item, expected in (("available", available, 200), ("gated", gated, 401), ("private", private, 403), ("expired", expired, 410)):
        status, headers, raw = public_get(item["public_slug"])
        expect(status == expected, f"{label} page status={status}")
        assert_noindex_headers(headers, f"{label} page")
        expect('<meta name="robots" content="noindex,nofollow">' in body_text(raw).lower(), f"{label} HTML robots missing")
        checks.append({"label":label,"surface":"html","status":status,"x_robots_tag":headers_lower(headers).get("x-robots-tag")})
        machine_status, machine_headers, _ = public_action(item["public_slug"])
        machine_expected = 200 if label == "available" else (403 if label in ("gated","private") else 410)
        expect(machine_status == machine_expected, f"{label} machine status={machine_status}")
        assert_noindex_headers(machine_headers, f"{label} machine")
        checks.append({"label":label,"surface":"machine","status":machine_status,"x_robots_tag":headers_lower(machine_headers).get("x-robots-tag")})
    unknown_status, unknown_headers, unknown_raw = public_get("p10-seo-unknown")
    expect(unknown_status == 404, f"unknown status={unknown_status}")
    assert_noindex_headers(unknown_headers, "unknown page")
    expect('<meta name="robots" content="noindex,nofollow">' in body_text(unknown_raw).lower(), "unknown HTML robots missing")
    return {"checks":checks,"unknown_status":unknown_status,"unknown_x_robots_tag":headers_lower(unknown_headers).get("x-robots-tag")}


def sitemap_hits():
    command = r'''files=$(find . -type f \( -iname '*sitemap*.xml' -o -iname '*sitemap*.txt' -o -iname '*sitemap*.json' \) -not -path './.git/*' -print); if [ -n "$files" ]; then grep -nH -E '(^|["> ])/?t/' $files || true; fi'''
    proc = subprocess.run(["bash", "-lc", command], text=True, capture_output=True, check=True)
    return [line for line in proc.stdout.splitlines() if line.strip()]


def case_t014():
    available = create_share("p10-t014-available", visibility="public", content="canonical-probe")
    status, _, raw = public_get(available["public_slug"])
    expect(status == 200, f"available status={status}")
    html = body_text(raw).lower()
    expect('rel="canonical"' not in html and "rel='canonical'" not in html, "PUB-TEXT emitted canonical")
    expect("hreflang=" not in html, "PUB-TEXT emitted hreflang")
    expect("application/ld+json" not in html, "PUB-TEXT emitted structured data")
    unknown_status = public_get("p10-t014-missing")[0]
    expect(unknown_status == 404, f"unknown soft-404 status={unknown_status}")
    expired = create_share("p10-t014-expired", visibility="public", content="expired", expires_at=past_time())
    expired_status = public_get(expired["public_slug"])[0]
    expect(expired_status == 410, f"expired soft-404 status={expired_status}")
    consumed = create_share("p10-t014-consumed", visibility="public", content="consume", one_time=True)
    expect(public_action(consumed["public_slug"])[0] == 200, "failed to seed consumed fixture")
    consumed_status = public_get(consumed["public_slug"])[0]
    expect(consumed_status == 410, f"consumed soft-404 status={consumed_status}")
    removed = create_share("p10-t014-removed", visibility="public", content="removed")
    expect(delete_share("p10-t014-removed", removed["id"], removed["version"])[0] == 204, "failed to seed removed fixture")
    removed_status = public_get(removed["public_slug"])[0]
    expect(removed_status == 410, f"removed soft-404 status={removed_status}")
    hits = sitemap_hits()
    expect(hits == [], f"Text UGC appeared in sitemap files: {hits}")
    return {"canonical_present":False,"hreflang_present":False,"structured_data_present":False,"sitemap_text_hits":hits,"statuses":{"available":status,"unknown":unknown_status,"expired":expired_status,"consumed":consumed_status,"removed":removed_status}}


CASES = {"P10-T013":case_t013,"P10-T014":case_t014}


def main():
    import argparse, sys
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=sorted(CASES))
    args = parser.parse_args()
    errors, observations = [], {}
    try:
        observations = CASES[args.case]()
    except Exception as exc:
        errors.append(f"{type(exc).__name__}: {exc}")
    path = record(args.case, observations, errors, HEADER_DIR)
    print(path)
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print(f"{args.case} PASS on {HEAD}")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
