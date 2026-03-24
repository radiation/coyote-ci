import { createBrowserRouter, Navigate } from 'react-router-dom';
import { Layout } from '../components/Layout';
import { BuildsListPage } from '../pages/BuildsListPage';
import { BuildDetailPage } from '../pages/BuildDetailPage';

export const router = createBrowserRouter([
  {
    element: <Layout />,
    children: [
      { path: '/', element: <Navigate to="/builds" replace /> },
      { path: '/builds', element: <BuildsListPage /> },
      { path: '/builds/:id', element: <BuildDetailPage /> },
    ],
  },
]);
