import { lazy, type ComponentType } from "react";
import {
  createBrowserRouter,
  Navigate,
  type RouteObject,
} from "react-router-dom";
import { Layout } from "../components/Layout";

function lazyPage<
  TModule extends Record<string, unknown>,
  TExport extends keyof TModule,
>(loader: () => Promise<TModule>, exportName: TExport) {
  return lazy(async () => {
    const module = await loader();
    return { default: module[exportName] as ComponentType };
  });
}

const DashboardPage = lazyPage(
  () => import("../pages/DashboardPage"),
  "DashboardPage",
);
const BuildsListPage = lazyPage(
  () => import("../pages/BuildsListPage"),
  "BuildsListPage",
);
const BuildDetailPage = lazyPage(
  () => import("../pages/BuildDetailPage"),
  "BuildDetailPage",
);
const QueuePage = lazyPage(() => import("../pages/QueuePage"), "QueuePage");
const WorkersPage = lazyPage(
  () => import("../pages/WorkersPage"),
  "WorkersPage",
);
const ArtifactsPage = lazyPage(
  () => import("../pages/ArtifactsPage"),
  "ArtifactsPage",
);
const ArtifactLogicalBrowserPage = lazyPage(
  () => import("../pages/ArtifactLogicalBrowserPage"),
  "ArtifactLogicalBrowserPage",
);
const ArtifactDetailPage = lazyPage(
  () => import("../pages/ArtifactDetailPage"),
  "ArtifactDetailPage",
);
const ProjectsListPage = lazyPage(
  () => import("../pages/ProjectsListPage"),
  "ProjectsListPage",
);
const ProjectDetailPage = lazyPage(
  () => import("../pages/ProjectDetailPage"),
  "ProjectDetailPage",
);
const JobsListPage = lazyPage(
  () => import("../pages/JobsListPage"),
  "JobsListPage",
);
const JobCreatePage = lazyPage(
  () => import("../pages/JobCreatePage"),
  "JobCreatePage",
);
const JobDetailPage = lazyPage(
  () => import("../pages/JobDetailPage"),
  "JobDetailPage",
);
const APITokensPage = lazyPage(
  () => import("../pages/APITokensPage"),
  "APITokensPage",
);
const ProfilePage = lazyPage(
  () => import("../pages/ProfilePage"),
  "ProfilePage",
);
const MyNotificationsPage = lazyPage(
  () => import("../pages/MyNotificationsPage"),
  "MyNotificationsPage",
);
const UsersPage = lazyPage(() => import("../pages/UsersPage"), "UsersPage");
const CredentialsPage = lazyPage(
  () => import("../pages/CredentialsPage"),
  "CredentialsPage",
);
const NotificationsPage = lazyPage(
  () => import("../pages/NotificationsPage"),
  "NotificationsPage",
);

export const appRoutes: RouteObject[] = [
  {
    element: <Layout />,
    children: [
      { path: "/", element: <Navigate to="/dashboard" replace /> },
      { path: "/dashboard", element: <DashboardPage /> },
      { path: "/builds", element: <BuildsListPage /> },
      { path: "/builds/:id", element: <BuildDetailPage /> },
      { path: "/queue", element: <QueuePage /> },
      { path: "/workers", element: <WorkersPage /> },
      { path: "/artifacts", element: <ArtifactsPage /> },
      { path: "/artifacts/logical", element: <ArtifactLogicalBrowserPage /> },
      { path: "/artifacts/:id", element: <ArtifactDetailPage /> },
      { path: "/projects", element: <ProjectsListPage /> },
      { path: "/projects/:id", element: <ProjectDetailPage /> },
      { path: "/jobs", element: <JobsListPage /> },
      { path: "/jobs/new", element: <JobCreatePage /> },
      { path: "/jobs/:id", element: <JobDetailPage /> },
      {
        path: "/settings/tokens",
        element: <Navigate to="/settings/my-api-tokens" replace />,
      },
      {
        path: "/settings/api-tokens",
        element: <Navigate to="/settings/my-api-tokens" replace />,
      },
      { path: "/settings/my-api-tokens", element: <APITokensPage /> },
      { path: "/settings/profile", element: <ProfilePage /> },
      { path: "/settings/my-notifications", element: <MyNotificationsPage /> },
      { path: "/settings/users", element: <UsersPage /> },
      { path: "/settings/credentials", element: <CredentialsPage /> },
      { path: "/settings/notifications", element: <NotificationsPage /> },
    ],
  },
];

export const router = createBrowserRouter(appRoutes);
