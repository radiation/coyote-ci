import { createBrowserRouter, Navigate } from "react-router-dom";
import { Layout } from "../components/Layout";
import { BuildsListPage } from "../pages/BuildsListPage";
import { BuildDetailPage } from "../pages/BuildDetailPage";
import { QueuePage } from "../pages/QueuePage";
import { ArtifactDetailPage } from "../pages/ArtifactDetailPage";
import { ArtifactLogicalBrowserPage } from "../pages/ArtifactLogicalBrowserPage";
import { ArtifactsPage } from "../pages/ArtifactsPage";
import { JobsListPage } from "../pages/JobsListPage";
import { JobCreatePage } from "../pages/JobCreatePage";
import { JobDetailPage } from "../pages/JobDetailPage";
import { CredentialsPage } from "../pages/CredentialsPage";
import { APITokensPage } from "../pages/APITokensPage";
import { UsersPage } from "../pages/UsersPage";
import { ProjectsListPage } from "../pages/ProjectsListPage";
import { ProjectDetailPage } from "../pages/ProjectDetailPage";
import { DashboardPage } from "../pages/DashboardPage";

export const appRoutes = [
  {
    element: <Layout />,
    children: [
      { path: "/", element: <Navigate to="/dashboard" replace /> },
      { path: "/dashboard", element: <DashboardPage /> },
      { path: "/builds", element: <BuildsListPage /> },
      { path: "/builds/:id", element: <BuildDetailPage /> },
      { path: "/queue", element: <QueuePage /> },
      { path: "/artifacts", element: <ArtifactsPage /> },
      { path: "/artifacts/logical", element: <ArtifactLogicalBrowserPage /> },
      { path: "/artifacts/:id", element: <ArtifactDetailPage /> },
      { path: "/projects", element: <ProjectsListPage /> },
      { path: "/projects/:id", element: <ProjectDetailPage /> },
      { path: "/jobs", element: <JobsListPage /> },
      { path: "/jobs/new", element: <JobCreatePage /> },
      { path: "/jobs/:id", element: <JobDetailPage /> },
      { path: "/settings/tokens", element: <APITokensPage /> },
      { path: "/settings/users", element: <UsersPage /> },
      { path: "/settings/credentials", element: <CredentialsPage /> },
    ],
  },
] as const;

export const router = createBrowserRouter(appRoutes);
