import { createBrowserRouter, Navigate } from 'react-router-dom';
import { Layout } from '../components/Layout';
import { BuildsListPage } from '../pages/BuildsListPage';
import { BuildDetailPage } from '../pages/BuildDetailPage';
import { JobsListPage } from '../pages/JobsListPage';
import { JobCreatePage } from '../pages/JobCreatePage';
import { JobDetailPage } from '../pages/JobDetailPage';

export const router = createBrowserRouter([
  {
    element: <Layout />,
    children: [
      { path: '/', element: <Navigate to="/jobs" replace /> },
      { path: '/builds', element: <BuildsListPage /> },
      { path: '/builds/:id', element: <BuildDetailPage /> },
      { path: '/jobs', element: <JobsListPage /> },
      { path: '/jobs/new', element: <JobCreatePage /> },
      { path: '/jobs/:id', element: <JobDetailPage /> },
    ],
  },
]);
