import { lazy, Suspense } from 'react';
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';
import { MarketingPage } from './routes/ShellPage';
import { AuthRoutePage } from './auth/AuthRoutePage';

const FoundationPage = lazy(() => import('./routes/FoundationPage'));
const ContactPage = lazy(() => import('./contact/ContactPage'));
const LinkUnavailablePage = lazy(() => import('./trust/LinkUnavailablePage'));
const AbuseReportPage = lazy(() => import('./trust/AbuseReportPage'));
const rootRoute = createRootRoute({ component: () => <Suspense fallback={<main aria-busy="true">Loading route…</main>}><Outlet /></Suspense> });
const homeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: () => <MarketingPage title="Product shell foundation" /> });
const productsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/products', component: () => <MarketingPage title="Products" /> });
const solutionsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/solutions', component: () => <MarketingPage title="Solutions" /> });
const developersRoute = createRoute({ getParentRoute: () => rootRoute, path: '/developers', component: () => <MarketingPage title="Developers" /> });
const pricingRoute = createRoute({ getParentRoute: () => rootRoute, path: '/pricing', component: () => <MarketingPage title="Pricing" /> });
const contactRoute = createRoute({ getParentRoute: () => rootRoute, path: '/contact', component: ContactPage });
const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: '/login', component: () => <AuthRoutePage kind="login" /> });
const registerRoute = createRoute({ getParentRoute: () => rootRoute, path: '/register', component: () => <AuthRoutePage kind="register" /> });
const verifyRoute = createRoute({ getParentRoute: () => rootRoute, path: '/verify-email', component: () => <AuthRoutePage kind="verify" /> });
const forgotRoute = createRoute({ getParentRoute: () => rootRoute, path: '/forgot-password', component: () => <AuthRoutePage kind="forgot" /> });
const resetRoute = createRoute({ getParentRoute: () => rootRoute, path: '/reset-password', component: () => <AuthRoutePage kind="reset" /> });
const oauthCallbackRoute = createRoute({ getParentRoute: () => rootRoute, path: '/oauth/$provider/callback', component: () => <AuthRoutePage kind="oauth" /> });
const socialRegistrationRoute = createRoute({ getParentRoute: () => rootRoute, path: '/social-registration', component: () => <AuthRoutePage kind="social" /> });
const linkUnavailableRoute = createRoute({ getParentRoute: () => rootRoute, path: '/linkunavailable', component: LinkUnavailablePage });
const abuseReportRoute = createRoute({ getParentRoute: () => rootRoute, path: '/abuse/report', component: AbuseReportPage });
const foundationRoute = createRoute({ getParentRoute: () => rootRoute, path: '/foundation', component: FoundationPage });
const routeTree = rootRoute.addChildren([
  homeRoute, productsRoute, solutionsRoute, developersRoute, pricingRoute, contactRoute,
  loginRoute, registerRoute, verifyRoute, forgotRoute, resetRoute, oauthCallbackRoute,
  socialRegistrationRoute, linkUnavailableRoute, abuseReportRoute, foundationRoute,
]);
export const router = createRouter({ routeTree, defaultPreload: 'intent' });
declare module '@tanstack/react-router' { interface Register { router: typeof router } }
