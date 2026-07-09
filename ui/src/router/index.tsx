import { Navigate, Outlet, createHashRouter } from 'react-router-dom'

import AppShellLayout from '../layouts/AppShellLayout'
import { hasAuthToken } from '../lib/auth/token'
import DashboardPage from '../pages/DashboardPage'
import EventsPage from '../pages/EventsPage'
import FilesPage from '../pages/FilesPage'
import LoginPage from '../pages/LoginPage'
import LogsPage from '../pages/LogsPage'
import ControllerLogsPage from '../pages/ControllerLogsPage'
import MetricsPage from '../pages/MetricsPage'
import PoolDetailPage from '../pages/PoolDetailPage'
import PoolListPage from '../pages/PoolListPage'
import RateLimitPage from '../pages/RateLimitPage'
import RuntimeConfigPage from '../pages/RuntimeConfigPage'
import SandboxesPage from '../pages/SandboxesPage'
import SandboxCommandsPage from '../pages/SandboxCommandsPage'
import SandboxTemplateConfigPage from '../pages/SandboxTemplateConfigPage'
import TemplatesConfigPage from '../pages/TemplatesConfigPage'
import TerminalPage from '../pages/TerminalPage'

function RequireAuth() {
  if (!hasAuthToken()) {
    return <Navigate to="/login" replace />
  }
  return <Outlet />
}

function RedirectIfAuthed() {
  if (hasAuthToken()) {
    return <Navigate to="/dashboard" replace />
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
            element: <Navigate to="dashboard" replace />,
          },
          {
            path: 'dashboard',
            element: <DashboardPage />,
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
            path: 'ratelimit',
            element: <RateLimitPage />,
          },
          {
            path: 'metrics',
            element: <MetricsPage />,
          },
          {
            path: 'controller-logs',
            element: <ControllerLogsPage />,
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
            path: 'events',
            element: <EventsPage />,
          },
          {
            path: 'sandbox-commands',
            element: <SandboxCommandsPage />,
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
          {
            path: 'config/runtime',
            element: <RuntimeConfigPage />,
          },
        ],
      },
    ],
  },
  {
    path: '*',
    element: <Navigate to="/dashboard" replace />,
  },
])
