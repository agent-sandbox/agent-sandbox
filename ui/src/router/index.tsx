import { Navigate, Outlet, createHashRouter } from 'react-router-dom'

import AppShellLayout from '../layouts/AppShellLayout'
import { hasAuthToken } from '../lib/auth/token'
import EventsPage from '../pages/EventsPage'
import FilesPage from '../pages/FilesPage'
import LoginPage from '../pages/LoginPage'
import LogsPage from '../pages/LogsPage'
import PoolDetailPage from '../pages/PoolDetailPage'
import PoolListPage from '../pages/PoolListPage'
import SandboxesPage from '../pages/SandboxesPage'
import SandboxTemplateConfigPage from '../pages/SandboxTemplateConfigPage'
import TemplatesConfigPage from '../pages/TemplatesConfigPage'
import TerminalPage from '../pages/TerminalPage'
import TrafficPage from '../pages/TrafficPage'

function RequireAuth() {
  if (!hasAuthToken()) {
    return <Navigate to="/login" replace />
  }
  return <Outlet />
}

function RedirectIfAuthed() {
  if (hasAuthToken()) {
    return <Navigate to="/sandboxes" replace />
  }
  return <LoginPage />
}

export const appRouter = createHashRouter([
  {
    path: '/login',
    element: <RedirectIfAuthed />,
  },
  {
    element: <RequireAuth />,
    children: [
      {
        path: '/',
        element: <AppShellLayout />,
        children: [
          {
            index: true,
            element: <Navigate to="sandboxes" replace />,
          },
          {
            path: 'sandboxes',
            element: <SandboxesPage />,
          },
          {
            path: 'pool',
            element: <PoolListPage />,
          },
          {
            path: 'logs',
            element: <LogsPage />,
          },
          {
            path: 'terminal',
            element: <TerminalPage />,
          },
          {
            path: 'files',
            element: <FilesPage />,
          },
          {
            path: 'traffic',
            element: <TrafficPage />,
          },
          {
            path: 'events',
            element: <EventsPage />,
          },
          {
            path: 'pool/:poolName',
            element: <PoolDetailPage />,
          },
          {
            path: 'config/templates',
            element: <TemplatesConfigPage />,
          },
          {
            path: 'config/sandbox-template',
            element: <SandboxTemplateConfigPage />,
          },
        ],
      },
    ],
  },
  {
    path: '*',
    element: <Navigate to="/sandboxes" replace />,
  },
])
