import { useEffect, useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'

import { canAccessNav, clearAuthToken, getAuthToken } from '../lib/auth/token'
import { applyTheme, getAvailableThemes, getTheme, type ThemeName } from '../lib/theme/theme'

export default function AppShellLayout() {
  const navigate = useNavigate()
  const [version, setVersion] = useState('unknown')
  const [theme, setTheme] = useState<ThemeName>(getTheme())
  const token = getAuthToken()

  const canViewDashboard = canAccessNav('dashboard', token)
  const canViewSandboxes = canAccessNav('sandboxes', token)
  const canViewPool = canAccessNav('pool', token)
  const canViewRateLimit = canAccessNav('rateLimit', token)
  const canViewMetrics = canAccessNav('metrics', token)
  const canViewControllerLogs = canAccessNav('controllerLogs', token)
  const canViewLogs = canAccessNav('logs', token)
  const canViewTerminal = canAccessNav('terminal', token)
  const canViewFiles = canAccessNav('files', token)
  const canViewTemplatesConfig = canAccessNav('templatesConfig', token)
  const canViewSandboxBlueprintConfig = canAccessNav('sandboxBlueprintConfig', token)
  const canViewRuntimeConfig = canAccessNav('runtimeConfig', token)
  const canViewEvents = canAccessNav('events', token)

  const hasSandboxTools = canViewLogs || canViewTerminal || canViewFiles
  const hasSettings = canViewTemplatesConfig || canViewSandboxBlueprintConfig || canViewRuntimeConfig || canViewEvents

  const tokenPreview = token ? `${token.substring(0, 10)}...` : 'N/A'

  const handleThemeChange = (nextTheme: ThemeName) => {
    setTheme(nextTheme)
    applyTheme(nextTheme)
  }

  const isThemeToggleChecked = theme === 'synthwave'

  const handleThemeToggleChange = (checked: boolean) => {
    handleThemeChange(checked ? 'synthwave' : 'light')
  }

  const handleLogout = () => {
    clearAuthToken()
    navigate('/login', { replace: true })
  }

  useEffect(() => {
    let cancelled = false

    const loadVersion = async () => {
      try {
        const response = await fetch('/healthz')
        if (!response.ok) {
          return
        }
        const payload = (await response.json()) as { version?: string }
        if (!cancelled && payload.version && payload.version.trim() !== '') {
          setVersion(payload.version.trim())
        }
      } catch {
        // keep fallback version
      }
    }

    loadVersion()

    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div className="h-screen  bg-base-200 text-base-content">
      <div className="mx-auto flex h-full w-full  gap-6 p-4 lg:p-6">
          {/*<aside className="sticky top-4 hidden h-[calc(100vh-2rem)] w-[260px] shrink-0 flex-col rounded-box border border-base-300 bg-base-100 p-4 shadow-sm lg:flex">*/}
        <aside className="sticky top-4 hidden h-[calc(100vh-2rem)] w-[260px] shrink-0 flex-col  p-4  lg:flex">
          <div className="mb-4 flex items-center gap-3">
            <img src={`${import.meta.env.BASE_URL}logo.png`} alt="Agent Sandbox logo" className="h-8 w-7 rounded" />
            <div>
              <h1 className="text-lg font-semibold">Agent Sandbox</h1>
              <p className="text-xs text-base-content/70">Dashboard</p>
            </div>
          </div>

            <ul className="menu w-full p-0">
                <li></li>
                {canViewDashboard && (
                  <li>
                      <NavLink to="/dashboard"
                               className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                          Dashboard
                      </NavLink>
                  </li>
                )}
                {canViewSandboxes && (
                  <li>
                      <NavLink to="/sandboxes"
                               className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                          Sandboxes
                      </NavLink>
                  </li>
                )}
                {canViewPool && (
                  <li>
                      <NavLink to="/pool" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                          Pools
                      </NavLink>
                  </li>
                )}
                {canViewRateLimit && (
                  <li>
                      <NavLink to="/ratelimit" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                          Capacity
                      </NavLink>
                  </li>
                )}
                {hasSandboxTools && <li></li>}
                {hasSandboxTools && <li className="menu-title">Sandbox Tools</li>}
                {canViewLogs && (
                  <li>
                      <NavLink to="/logs" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                          Logs
                      </NavLink>
                  </li>
                )}
                {canViewTerminal && (
                  <li>
                      <NavLink to="/terminal"
                               className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                          Terminal
                      </NavLink>
                  </li>
                )}
                {canViewFiles && (
                  <li>
                      <NavLink to="/files" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                          Files
                      </NavLink>
                  </li>
                )}
                {hasSettings && <li></li>}
                {hasSettings && <li className="menu-title">Settings</li>}
                {canViewRuntimeConfig && (
                  <li>
                      <NavLink to="/config/runtime"
                               className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                          Runtime
                      </NavLink>
                  </li>
                )}
                {canViewTemplatesConfig && (
                  <li>
                      <NavLink to="/config/templates"
                               className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                          Templates
                      </NavLink>
                  </li>
                )}
                {canViewSandboxBlueprintConfig && (
                  <li>
                      <NavLink to="/config/blueprint"
                               className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                          Sandbox-Blueprint
                      </NavLink>
                  </li>
                )}
                <li></li>
                <li className="menu-title">Monitoring</li>
                {canViewMetrics && (
                    <li>
                        <NavLink to="/metrics" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                            Metrics
                        </NavLink>
                    </li>
                )}
                {canViewControllerLogs && (
                    <li>
                        <NavLink to="/controller-logs" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                            Logs
                        </NavLink>
                    </li>
                )}
                {canViewEvents && (
                  <li>
                      <NavLink to="/events" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}>
                          Events
                      </NavLink>
                  </li>
                )}
            </ul>

            <div className="mt-auto space-y-3">
                <div className="mt-4">
                    <label className="label py-1" htmlFor="theme-controller">
                        <span className="label-text text-xs">Theme: </span>
                    </label>
                    <div className="flex items-center gap-4">
                    <div>
                        <label className="flex cursor-pointer gap-2">
                            <svg
                                xmlns="http://www.w3.org/2000/svg"
                                width="15"
                                height="15"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                strokeWidth="2"
                                strokeLinecap="round"
                                strokeLinejoin="round">
                                <circle cx="12" cy="12" r="5"/>
                                <path
                                    d="M12 1v2M12 21v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M1 12h2M21 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4"/>
                            </svg>
                            <input
                                type="checkbox"
                                value="synthwave"
                                className="toggle checkbox-xs theme-controller"
                                checked={isThemeToggleChecked}
                                onChange={(event) => handleThemeToggleChange(event.target.checked)}
                            />
                            <svg
                                xmlns="http://www.w3.org/2000/svg"
                                width="15"
                                height="15"
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                strokeWidth="2"
                                strokeLinecap="round"
                                strokeLinejoin="round">
                                <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path>
                            </svg>
                        </label>
                    </div>
                    <div>
                    <select
                        id="theme-controller"
                        className="select select-bordered select-xs w-30"
                        value={theme}
                        onChange={(event) => handleThemeChange(event.target.value as ThemeName)}
                    >
                        {getAvailableThemes().map((item) => (
                            <option key={item} value={item}>
                                {item}
                            </option>
                        ))}
                    </select>
                    </div>
                    </div>
                </div>
                <button type="button" className="btn btn-sm btn-outline w-full" onClick={handleLogout}>
                    Logout
                </button>
                <div className="text-center">
                    <div className="status status-info "></div>
                    <span className="text-xs text-base-content/70"> Token: {tokenPreview}</span>
                </div>
                <div className=" text-xs text-base-content/70 text-center">
                    Agent-Sandbox Version v{version}
                </div>
            </div>
        </aside>


          <main className="custom-scrollbar min-h-0 min-w-0 flex-1 space-y-3 overflow-y-auto"
                style={{paddingRight: '20px'}}>
              <Outlet/>
          </main>
      </div>
    </div>
  )
}
