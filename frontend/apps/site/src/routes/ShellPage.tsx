import { Link } from '@tanstack/react-router';
import { Button, TextField } from '@gojet/ui';
import { AuthShell, WebsiteShell } from '../shell/SiteShells';

export function MarketingPage({ title }: { title: string }) {
  return <WebsiteShell><section className="site-page"><p className="site-eyebrow">GoJet V10</p><h1>{title}</h1><p>This P04 surface validates the shared Website shell. Deep product behavior is implemented by its owning vertical node.</p><div className="site-page-actions"><Link to="/pricing">View pricing shell</Link><a href="/docs/en/">Read docs</a></div></section></WebsiteShell>;
}

export function LoginPage({ mode }: { mode: 'login' | 'register' }) {
  const registering = mode === 'register';
  return <AuthShell><form className="auth-form" onSubmit={(event) => event.preventDefault()}><h2>{registering ? 'Create account' : 'Sign in'}</h2><TextField id="auth-email" label="Email" type="email" autoComplete="email"/><TextField id="auth-password" label="Password" type="password" autoComplete={registering ? 'new-password' : 'current-password'}/><Button type="submit">{registering ? 'Create account' : 'Sign in'}</Button><p>{registering ? <>Already registered? <Link to="/login">Sign in</Link></> : <>New to GoJet? <Link to="/register">Create account</Link></>}</p></form></AuthShell>;
}
