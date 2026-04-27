import { createBrowserRouter, Navigate } from "react-router-dom";
import { Layout } from "../components/Layout";
import { BuildsListPage } from "../pages/BuildsListPage";
import { BuildDetailPage } from "../pages/BuildDetailPage";
import { ArtifactsPage } from "../pages/ArtifactsPage";
import { JobsListPage } from "../pages/JobsListPage";
import { JobCreatePage } from "../pages/JobCreatePage";
import { JobDetailPage } from "../pages/JobDetailPage";
import { CredentialsPage } from "../pages/CredentialsPage";

export const router = createBrowserRouter([
  {
    element: <Layout />,
    children: [
      { path: "/", element: <Navigate to="/jobs" replace /> },
      { path: "/builds", element: <BuildsListPage /> },
      { path: "/builds/:id", element: <BuildDetailPage /> },
      { path: "/artifacts", element: <ArtifactsPage /> },
      { path: "/jobs", element: <JobsListPage /> },
      { path: "/jobs/new", element: <JobCreatePage /> },
      { path: "/jobs/:id", element: <JobDetailPage /> },
      { path: "/settings/credentials", element: <CredentialsPage /> },
    ],
  },
]);
