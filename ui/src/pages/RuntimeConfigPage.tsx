import { useEffect, useState } from 'react'

import { getRuntimeConfigStatus, saveRuntimeConfig } from '../lib/api/config'
import type { RuntimeConfigPayload, RuntimeConfigStatus } from '../lib/api/types'

type RuntimeConfigDraft = Omit<RuntimeConfigStatus, 'rate_limit'> & {
  rate_limit: {
    enabled: boolean
    max_concurrency: string
    max_sandbox: string
  }
}

const emptyRuntimeConfig: RuntimeConfigDraft = {
  config_map_key: 'config-runtime',
  system_token: '',
  api_tokens_raw: '',
  rate_limit: {
    enabled: false,
    max_concurrency: '0',
    max_sandbox: '0',
  },
  rate_limit_users_raw: '',
  rate_limit_users: [],
  sandbox_default_image: '',
  sandbox_default_template: '',
}

function parseNonNegativeInteger(value: string, fieldName: string): number {
  const trimmed = value.trim()
  if (trimmed === '') {
    return 0
  }

  const parsed = Number.parseInt(trimmed, 10)
  if (Number.isNaN(parsed) || parsed < 0 || String(parsed) !== trimmed) {
    throw new Error(`${fieldName} must be a non-negative integer.`)
  }
  return parsed
}

function formatJsonArray(value: string): string {
  const trimmed = value.trim()
  if (trimmed === '') {
    return ''
  }

  try {
    const parsed = JSON.parse(trimmed)
    return Array.isArray(parsed) ? JSON.stringify(parsed, null, 2) : value
  } catch {
    return value
  }
}

// Convert comma-separated tokens to newline-separated for display
function formatApiTokensRaw(value: string): string {
  return value.trim().replace(/,/g, '\n')
}

// Convert newline-separated tokens back to comma-separated for saving
function normalizeApiTokensRaw(value: string): string {
  return value.split('\n').map((t) => t.trim()).filter(Boolean).join(',')
}

function toRuntimeConfigPayload(draft: RuntimeConfigDraft): RuntimeConfigPayload {
  const rateLimitUsersRaw = draft.rate_limit_users_raw.trim()
  if (rateLimitUsersRaw !== '') {
    try {
      const parsed = JSON.parse(rateLimitUsersRaw)
      if (!Array.isArray(parsed)) {
        throw new Error('rate_limit_users_raw must be a JSON array.')
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'rate_limit_users_raw is invalid JSON.'
      throw new Error(message)
    }
  }

  const maxConcurrency = parseNonNegativeInteger(String(draft.rate_limit.max_concurrency), 'Max Concurrency')
  const maxSandbox = parseNonNegativeInteger(String(draft.rate_limit.max_sandbox), 'Max Sandbox')

  return {
    system_token: draft.system_token.trim(),
    api_tokens_raw: normalizeApiTokensRaw(draft.api_tokens_raw),
    rate_limit: {
      enabled: draft.rate_limit.enabled,
      max_concurrency: maxConcurrency,
      max_sandbox: maxSandbox,
    },
    rate_limit_users_raw: rateLimitUsersRaw,
    sandbox_default_image: draft.sandbox_default_image.trim(),
    sandbox_default_template: draft.sandbox_default_template.trim(),
  }
}

export default function RuntimeConfigPage() {
  const [runtimeConfig, setRuntimeConfig] = useState<RuntimeConfigDraft>(emptyRuntimeConfig)
  const [isLoading, setIsLoading] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [saveError, setSaveError] = useState('')
  const [saveSuccess, setSaveSuccess] = useState('')

  const applyRuntimeConfigStatus = (data: RuntimeConfigStatus) => {
    setRuntimeConfig({
      ...emptyRuntimeConfig,
      ...data,
      api_tokens_raw: formatApiTokensRaw(data.api_tokens_raw),
      rate_limit_users_raw: formatJsonArray(data.rate_limit_users_raw),
      rate_limit_users: Array.isArray(data.rate_limit_users) ? data.rate_limit_users : [],
      rate_limit: {
        enabled: data.rate_limit.enabled,
        max_concurrency: String(data.rate_limit.max_concurrency),
        max_sandbox: String(data.rate_limit.max_sandbox),
      },
    })
  }

  const loadRuntimeConfig = async (options?: { keepMessages?: boolean }) => {
    setIsLoading(true)
    setLoadError('')

    if (!options?.keepMessages) {
      setSaveError('')
      setSaveSuccess('')
    }

    try {
      const data = await getRuntimeConfigStatus()
      applyRuntimeConfigStatus(data)
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to load runtime config'
      setLoadError(message)
      setRuntimeConfig(emptyRuntimeConfig)
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    void loadRuntimeConfig()
  }, [])

  const handleSave = async () => {
    setIsSaving(true)
    setSaveError('')
    setSaveSuccess('')

    try {
      const saved = await saveRuntimeConfig(toRuntimeConfigPayload(runtimeConfig))
      applyRuntimeConfigStatus(saved)
      setSaveSuccess('Runtime config saved successfully.')
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to save runtime config'
      setSaveError(message)
    } finally {
      setIsSaving(false)
    }
  }

  const updateRuntimeConfig = (updater: (previous: RuntimeConfigDraft) => RuntimeConfigDraft) => {
    setRuntimeConfig(updater)
    setSaveError('')
    setSaveSuccess('')
  }

  return (
    <>
      <header className="card border border-base-300 bg-base-100 shadow-sm">
        <div className="card-body gap-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-2xl font-semibold">Runtime Config</h2>
              <p className="text-sm text-base-content/70">Manage runtime settings directly from the ConfigMap. Changes take effect immediately after saving.</p>
            </div>
            <div className="flex items-center gap-2">
              <button
                className={`btn btn-sm btn-outline ${isLoading ? 'btn-disabled' : ''}`}
                type="button"
                onClick={() => {
                  void loadRuntimeConfig({ keepMessages: true })
                }}
                disabled={isLoading || isSaving}
              >
                {isLoading ? 'Reloading...' : 'Reload'}
              </button>
              <button
                className={`btn btn-sm btn-primary ${isSaving ? 'btn-disabled' : ''}`}
                type="button"
                onClick={() => {
                  void handleSave()
                }}
                disabled={isSaving || isLoading}
              >
                {isSaving ? 'Saving...' : 'Save Runtime Config'}
              </button>
            </div>
          </div>
        </div>
      </header>

      {(loadError || saveError || saveSuccess) && (
        <section>
          <div className="space-y-2">
            {loadError && (
              <div className="alert alert-error">
                <span>{loadError}</span>
              </div>
            )}
            {saveError && (
              <div className="alert alert-error">
                <span>{saveError}</span>
              </div>
            )}
            {saveSuccess && (
              <div className="alert alert-success">
                <span>{saveSuccess}</span>
              </div>
            )}
          </div>
        </section>
      )}

      <section className="grid gap-3 md:grid-cols-2">
        <div className="card border border-base-300 bg-base-100 shadow-sm">
          <div className="card-body gap-2 py-4">
            <h3 className="card-title text-lg">Capacity Limit</h3>
            <div className="stat p-0">
              <div className={`stat-value text-3xl ${runtimeConfig.rate_limit.enabled ? 'text-success' : 'text-warning'}`}>{runtimeConfig.rate_limit.enabled ? 'Enabled' : 'Disabled'}</div>
              <div className="stat-desc">Sandbox creation guard</div>
            </div>
          </div>
        </div>
        <div className="card border border-base-300 bg-base-100 shadow-sm">
          <div className="card-body gap-2 py-4">
            <h3 className="card-title text-lg">User Overrides</h3>
            <div className="stat p-0">
              <div className="stat-value text-3xl">{runtimeConfig.rate_limit_users.length}</div>
              <div className="stat-desc">Configured rate limit users</div>
            </div>
          </div>
        </div>
      </section>

      <section className="grid gap-3">
        <div className="card border border-base-300 bg-base-100 shadow-sm">
          <div className="card-body gap-4">
            <h3 className="card-title text-lg">Runtime Values</h3>
            <div className="grid gap-3 md:grid-cols-2">
              <label className="form-control w-full md:col-span-2">
                <div className="label">
                  <span className="label-text">System Token</span>
                </div>
                <input
                  className="input input-sm input-bordered w-full font-mono"
                  type="text"
                  value={runtimeConfig.system_token}
                  onChange={(event) => updateRuntimeConfig((prev) => ({ ...prev, system_token: event.target.value }))}
                />
              </label>

              <label className="form-control w-full md:col-span-2">
                <div className="label">
                  <span className="label-text">API Tokens Raw  (One per line)</span>
                </div>
                <textarea
                  className="textarea textarea-sm textarea-bordered min-h-[400px] w-full font-mono text-xs"
                  value={runtimeConfig.api_tokens_raw}
                  onChange={(event) => updateRuntimeConfig((prev) => ({ ...prev, api_tokens_raw: event.target.value }))}
                  placeholder="token-a&#10;token-b"
                />
              </label>

              <label className="form-control w-full">
                <div className="label">
                  <span className="label-text">Sandbox Default Image</span>
                </div>
                <input
                  className="input input-sm input-bordered w-full font-mono"
                  type="text"
                  value={runtimeConfig.sandbox_default_image}
                  onChange={(event) => updateRuntimeConfig((prev) => ({ ...prev, sandbox_default_image: event.target.value }))}
                />
              </label>

              <label className="form-control w-full">
                <div className="label">
                  <span className="label-text">Sandbox Default Template</span>
                </div>
                <input
                  className="input input-sm input-bordered w-full font-mono"
                  type="text"
                  value={runtimeConfig.sandbox_default_template}
                  onChange={(event) => updateRuntimeConfig((prev) => ({ ...prev, sandbox_default_template: event.target.value }))}
                />
              </label>
            </div>
          </div>
        </div>

        <div className="card border border-base-300 bg-base-100 shadow-sm">
          <div className="card-body gap-5">
            <h3 className="card-title text-lg">Capacity Config</h3>

            <div className="grid gap-3">
              <h4 className="text-sm font-semibold text-base-content/70">Default Config</h4>
                <div>
                    <label className="form-control w-full">
                        <div className="label">
                            <span className="label-text">Enabled</span>&nbsp;
                        </div>
                        <input
                            className="toggle toggle-sm"
                            type="checkbox"
                            checked={runtimeConfig.rate_limit.enabled}
                            onChange={(event) => updateRuntimeConfig((prev) => ({ ...prev, rate_limit: { ...prev.rate_limit, enabled: event.target.checked } }))}
                        />
                    </label>
                </div>
              <div className="grid gap-3 md:grid-cols-3">
                <label className="form-control w-full">
                  <div className="label">
                    <span className="label-text">Max Concurrency</span>
                  </div>
                  <input
                    className="input input-sm input-bordered w-full"
                    type="number"
                    min="0"
                    value={runtimeConfig.rate_limit.max_concurrency}
                    onChange={(event) => updateRuntimeConfig((prev) => ({ ...prev, rate_limit: { ...prev.rate_limit, max_concurrency: event.target.value } }))}
                  />
                </label>

                <label className="form-control w-full">
                  <div className="label">
                    <span className="label-text">Max Sandbox</span>
                  </div>
                  <input
                    className="input input-sm input-bordered w-full"
                    type="number"
                    min="0"
                    value={runtimeConfig.rate_limit.max_sandbox}
                    onChange={(event) => updateRuntimeConfig((prev) => ({ ...prev, rate_limit: { ...prev.rate_limit, max_sandbox: event.target.value } }))}
                  />
                </label>
              </div>
            </div>

            <div className="grid gap-3">
              <h4 className="text-sm font-semibold text-base-content/70">Users Config</h4>
              <label className="form-control w-full">
                <div className="label">
                  <span className="label-text">Users Capacity Raw</span>
                </div>
                <textarea
                  className="textarea textarea-sm textarea-bordered min-h-[500px] w-full font-mono text-xs"
                  value={runtimeConfig.rate_limit_users_raw}
                  onChange={(event) => updateRuntimeConfig((prev) => ({ ...prev, rate_limit_users_raw: event.target.value }))}
                  placeholder='[{"user":"token-a","max_concurrency":10,"max_sandbox":100}]'
                />
                <div className="label py-1">
                  <span className="label-text-alt text-base-content/70">Use a JSON array. Empty means no user-specific overrides.</span>
                </div>
              </label>

            </div>
          </div>
        </div>
      </section>
    </>
  )
}
