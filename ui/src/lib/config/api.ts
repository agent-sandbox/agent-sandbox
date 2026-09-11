const envBaseUrl = import.meta.env.VITE_API_BASE_URL?.trim()

// The path prefix the app is mounted under, derived from the page's own path
// instead of assumed to be the domain root. This keeps the app working when a
// reverse proxy mounts it under an extra prefix (e.g. https://host/agent-sandbox/ui/
// -> prefix "/agent-sandbox"), since the prefix is preserved in window.location
// but unknown at build time. Any other same-origin backend route (e.g. /healthz)
// should be fetched via `${getAppBasePath()}/healthz` for the same reason.
export function getAppBasePath(): string {
  return window.location.pathname
    .replace(/index\.html$/, '')
    .replace(/\/ui\/?$/, '')
    .replace(/\/$/, '')
}

function computeDefaultApiBaseUrl(): string {
  return `${getAppBasePath()}/api/v1`
}

export const API_BASE_URL = (envBaseUrl && envBaseUrl.length > 0 ? envBaseUrl : computeDefaultApiBaseUrl()).replace(/\/$/, '')
