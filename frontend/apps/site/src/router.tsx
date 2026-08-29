import { lazy, Suspense } from 'react';
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';
import { AuthRoutePage } from './auth/AuthRoutePage';
import { WebsitePage, websitePages } from './website/WebsitePage';

const ContactPage = lazy(() => import('./contact/ContactPage'));
const LinkUnavailablePage = lazy(() => import('./trust/LinkUnavailablePage'));
const AbuseReportPage = lazy(() => import('./trust/AbuseReportPage'));
const rootRoute = createRootRoute({ component: () => <Suspense fallback={<main aria-busy="true">Loading route…</main>}><Outlet /></Suspense> });

const homeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: () => <WebsitePage pathname="/" /> });
const zhHomeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/zh-CN/', component: () => <WebsitePage pathname="/zh-CN/" /> });
const productsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/products', component: () => <WebsitePage pathname="/products" /> });
const solutionsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/solutions', component: () => <WebsitePage pathname="/solutions" /> });
const developersRoute = createRoute({ getParentRoute: () => rootRoute, path: '/developers', component: () => <WebsitePage pathname="/developers" /> });
const pricingRoute = createRoute({ getParentRoute: () => rootRoute, path: '/pricing', component: () => <WebsitePage pathname="/pricing" /> });
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

const explicit = new Set(['/', '/zh-CN/', '/products', '/solutions', '/developers', '/pricing', '/contact']);
const websiteRoutes = websitePages.flatMap((page) => [page.path, page.zhPath])
  .filter((path) => !explicit.has(path))
  .map((path) => createRoute({ getParentRoute: () => rootRoute, path: path as any, component: () => <WebsitePage pathname={path} /> }));

const routeTree = rootRoute.addChildren([
  homeRoute, zhHomeRoute, productsRoute, solutionsRoute, developersRoute, pricingRoute,
  ...websiteRoutes, contactRoute, loginRoute, registerRoute, verifyRoute, forgotRoute, resetRoute,
  oauthCallbackRoute, socialRegistrationRoute, linkUnavailableRoute, abuseReportRoute,
]);
export const router = createRouter({ routeTree, defaultPreload: 'intent' });
declare module '@tanstack/react-router' { interface Register { router: typeof router } }
