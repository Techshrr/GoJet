import { lazy, Suspense } from 'react';
import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';
import { LoginPage, MarketingPage } from './routes/ShellPage';

const FoundationPage = lazy(() => import('./routes/FoundationPage'));
const rootRoute = createRootRoute({ component: () => <Suspense fallback={<main aria-busy="true">Loading route…</main>}><Outlet /></Suspense> });
const homeRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: () => <MarketingPage title="Product shell foundation" /> });
const productsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/products', component: () => <MarketingPage title="Products" /> });
const solutionsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/solutions', component: () => <MarketingPage title="Solutions" /> });
const developersRoute = createRoute({ getParentRoute: () => rootRoute, path: '/developers', component: () => <MarketingPage title="Developers" /> });
const pricingRoute = createRoute({ getParentRoute: () => rootRoute, path: '/pricing', component: () => <MarketingPage title="Pricing" /> });
const loginRoute = createRoute({ getParentRoute: () => rootRoute, path: '/login', component: () => <LoginPage mode="login" /> });
const registerRoute = createRoute({ getParentRoute: () => rootRoute, path: '/register', component: () => <LoginPage mode="register" /> });
const foundationRoute = createRoute({ getParentRoute: () => rootRoute, path: '/foundation', component: FoundationPage });
const routeTree = rootRoute.addChildren([homeRoute, productsRoute, solutionsRoute, developersRoute, pricingRoute, loginRoute, registerRoute, foundationRoute]);
export const router = createRouter({ routeTree, defaultPreload: 'intent' });
declare module '@tanstack/react-router' { interface Register { router: typeof router } }
