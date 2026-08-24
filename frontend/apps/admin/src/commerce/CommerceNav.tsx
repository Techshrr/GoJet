import { Link } from '@tanstack/react-router';

export function CommerceNav() {
  return <nav className="commerce-tabs" aria-label="Commerce sections">
    <Link to="/admin/commerce/plans">Plans</Link>
    <Link to="/admin/commerce/payments">Payments</Link>
    <Link to="/admin/commerce/fx">FX</Link>
  </nav>;
}
