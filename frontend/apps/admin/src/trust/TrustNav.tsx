const items = [
  ['Destination risk', '/admin/trust/destination-risk'],
  ['Domain risk', '/admin/trust/domain-risk'],
  ['Abuse', '/admin/trust/abuse'],
] as const;

export function TrustNav() {
  return (
    <nav className="trust-tabs" aria-label="Trust and Safety">
      {items.map(([label, href]) => <a key={href} href={href}>{label}</a>)}
    </nav>
  );
}

export function TrustState({ value }: { value: string }) {
  return <span className="trust-state" data-state={value}>{value}</span>;
}
