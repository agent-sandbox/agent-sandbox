import { useEffect, useRef, useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'

import { canAccessNav, clearAuthToken, getAuthToken } from '../lib/auth/token'
import { applyTheme, getAvailableThemes, getTheme, type ThemeName } from '../lib/theme/theme'

export default function AppShellLayout() {
  const navigate = useNavigate()
  const [version, setVersion] = useState('unknown')
  const [theme, setTheme] = useState<ThemeName>(getTheme())
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false)
  const drawerCheckboxRef = useRef<HTMLInputElement>(null)
  const token = getAuthToken()

  const canViewSandboxes = canAccessNav('sandboxes', token)
  const canViewPool = canAccessNav('pool', token)
  const canViewLogs = canAccessNav('logs', token)
  const canViewTerminal = canAccessNav('terminal', token)
  const canViewFiles = canAccessNav('files', token)
  const canViewTraffic = canAccessNav('traffic', token)
  const canViewTemplatesConfig = canAccessNav('templatesConfig', token)
  const canViewSandboxTemplateConfig = canAccessNav('sandboxTemplateConfig', token)
  const canViewEvents = canAccessNav('events', token)

  const hasSandboxTools = canViewLogs || canViewTerminal || canViewFiles || canViewTraffic
  const hasSettings = canViewTemplatesConfig || canViewSandboxTemplateConfig || canViewEvents

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

  const closeMobileMenu = () => {
    setIsMobileMenuOpen(false)
    if (drawerCheckboxRef.current) {
      drawerCheckboxRef.current.checked = false
    }
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
    <div className="drawer lg:drawer-open h-screen bg-base-200 text-base-content">
      <input
        id="main-drawer"
        type="checkbox"
        className="drawer-toggle"
        ref={drawerCheckboxRef}
        checked={isMobileMenuOpen}
        onChange={(e) => setIsMobileMenuOpen(e.target.checked)}
      />

      {/* Main content area */}
      <div className="drawer-content flex flex-col min-w-0">
        {/* Mobile topbar */}
        <div className="navbar sticky top-0 z-30 border-b border-base-300 bg-base-100 lg:hidden min-h-12 px-3">
          <label htmlFor="main-drawer" className="btn btn-ghost btn-square btn-sm">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <line x1="3" y1="6" x2="21" y2="6"/>
              <line x1="3" y1="12" x2="21" y2="12"/>
              <line x1="3" y1="18" x2="21" y2="18"/>
            </svg>
          </label>
          <div className="flex items-center gap-2 ml-2">
            <img src={`${import.meta.env.BASE_URL}logo.png`} alt="Agent Sandbox logo" className="h-6 w-5 rounded" />
            <span className="font-semibold text-sm">Agent Sandbox</span>
          </div>
        </div>

        <main className="custom-scrollbar flex-1 overflow-y-auto p-4 pr-5 lg:p-6 space-y-3 min-w-0">
          <Outlet/>
        </main>
      </div>

      {/* Sidebar drawer */}
      <div className="drawer-side z-40">
        <label htmlFor="main-drawer" aria-label="close sidebar" className="drawer-overlay"></label>
        <aside className="w-[260px] min-h-full bg-base-200 flex flex-col p-4">
          <div className="mb-4 flex items-center gap-3">
            <img src={`${import.meta.env.BASE_URL}logo.png`} alt="Agent Sandbox logo" className="h-8 w-7 rounded" />
            <div>
              <h1 className="text-lg font-semibold">Agent Sandbox</h1>
              <p className="text-xs text-base-content/70">Dashboard</p>
            </div>
          </div>

            <ul className="menu w-full p-0">
                <li></li>
                {canViewSandboxes && (
                  <li>
                      <NavLink to="/sandboxes"
                               className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}
                               onClick={closeMobileMenu}>
                          Sandboxes
                      </NavLink>
                  </li>
                )}
                {canViewPool && (
                  <li>
                      <NavLink to="/pool" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}
                               onClick={closeMobileMenu}>
                          Pools
                      </NavLink>
                  </li>
                )}
                {hasSandboxTools && <li></li>}
                {hasSandboxTools && <li className="menu-title">Sandbox Tools</li>}
                {canViewLogs && (
                  <li>
                      <NavLink to="/logs" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}
                               onClick={closeMobileMenu}>
                          Logs
                      </NavLink>
                  </li>
                )}
                {canViewTerminal && (
                  <li>
                      <NavLink to="/terminal"
                               className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}
                               onClick={closeMobileMenu}>
                          Terminal
                      </NavLink>
                  </li>
                )}
                {canViewFiles && (
                  <li>
                      <NavLink to="/files" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}
                               onClick={closeMobileMenu}>
                          Files
                      </NavLink>
                  </li>
                )}
                {canViewTraffic && (
                  <li>
                      <NavLink to="/traffic" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}
                               onClick={closeMobileMenu}>
                          Traffic
                      </NavLink>
                  </li>
                )}
                {hasSettings && <li></li>}
                {hasSettings && <li className="menu-title">Settings</li>}
                {canViewTemplatesConfig && (
                  <li>
                      <NavLink to="/config/templates"
                               className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}
                               onClick={closeMobileMenu}>
                          Templates Config
                      </NavLink>
                  </li>
                )}
                {canViewSandboxTemplateConfig && (
                  <li>
                      <NavLink to="/config/sandbox-template"
                               className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}
                               onClick={closeMobileMenu}>
                          Sandbox-Template Config
                      </NavLink>
                  </li>
                )}
                {canViewEvents && (
                  <li>
                      <NavLink to="/events" className={({isActive}) => (isActive ? 'menu-active text-left' : 'text-left')}
                               onClick={closeMobileMenu}>
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
      </div>
    </div>
  )
}
