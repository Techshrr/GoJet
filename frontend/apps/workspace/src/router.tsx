import { createRootRoute,createRoute,createRouter,Outlet } from '@tanstack/react-router';
import ShellPage from './routes/ShellPage';
const rootRoute=createRootRoute({component:Outlet});
const appRoute=createRoute({getParentRoute:()=>rootRoute,path:'/app',component:ShellPage});
const sectionRoutes=['links','qr','files','analytics','domains','developer','members','settings'].map((section)=>createRoute({getParentRoute:()=>rootRoute,path:`/app/${section}`,component:ShellPage}));
const routeTree=rootRoute.addChildren([appRoute,...sectionRoutes]); export const router=createRouter({routeTree,defaultPreload:'intent'}); declare module '@tanstack/react-router'{interface Register{router:typeof router}}
