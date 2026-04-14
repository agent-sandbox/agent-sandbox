const TOKEN_STORAGE_KEY = 'api-token'

export type NavKey =
  | 'sandboxes'
  | 'pool'
  | 'logs'
  | 'terminal'
  | 'files'
  | 'traffic'
  | 'templatesConfig'
  | 'sandboxTemplateConfig'
  | 'events'

const DEFAULT_ALLOWED_NAVS: NavKey[] = ['sandboxes', 'pool', 'logs', 'terminal', 'files', 'traffic', 'events']

export function canAccessNav(key: NavKey, token = getAuthToken()): boolean {
  if (token.startsWith('sys-')) {
    return true
  }

  return DEFAULT_ALLOWED_NAVS.includes(key)
}

export function getAuthToken(): string {
  try {
    return (localStorage.getItem(TOKEN_STORAGE_KEY) || '').trim()
  } catch {
    return ''
  }
}

export function setAuthToken(token: string): void {
  localStorage.setItem(TOKEN_STORAGE_KEY, token.trim())
}

export function clearAuthToken(): void {
  localStorage.removeItem(TOKEN_STORAGE_KEY)
}

export function hasAuthToken(): boolean {
  return getAuthToken() !== ''
}
