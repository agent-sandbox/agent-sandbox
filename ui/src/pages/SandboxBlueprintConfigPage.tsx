import { useEffect, useState } from 'react'

import { getSandboxBlueprintConfig, saveSandboxBlueprintConfig } from '../lib/api/config'

export default function SandboxBlueprintConfigPage() {
  const [blueprintText, setBlueprintText] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [saveError, setSaveError] = useState('')
  const [saveSuccess, setSaveSuccess] = useState('')

  const loadBlueprint = async (options?: { keepMessages?: boolean }) => {
    setIsLoading(true)
    setLoadError('')

    if (!options?.keepMessages) {
      setSaveError('')
      setSaveSuccess('')
    }

    try {
      const content = await getSandboxBlueprintConfig()
      setBlueprintText(content ?? '')
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to load sandbox blueprint config'
      setLoadError(message)
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    void loadBlueprint()
  }, [])

  const handleReload = async () => {
    await loadBlueprint({ keepMessages: true })
  }

  const handleSave = async () => {
    setIsSaving(true)
    setSaveError('')
    setSaveSuccess('')

    try {
      await saveSandboxBlueprintConfig(blueprintText)
      setSaveSuccess('Sandbox blueprint saved successfully.')
      await loadBlueprint({ keepMessages: true })
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to save sandbox blueprint config'
      setSaveError(message)
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <>
      <header className="card border border-base-300 bg-base-100 shadow-sm">
        <div className="card-body gap-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-2xl font-semibold">Sandbox-Blueprint Config</h2>
              <p className="text-sm text-base-content/70">View, edit, and save sandbox deploy ReplicaSet blueprint.</p>
            </div>
          </div>
        </div>
      </header>

      <section id="sandbox-blueprint-config">
        <div className="card border border-base-300 bg-base-100 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex items-center justify-between gap-2">
              <h3 className="card-title text-lg">Sandbox-Blueprint Config
                  <span className="label-text text-sm text-base-content/70"> - ReplicaSet Blueprint (Go Template + YAML)</span>
              </h3>
              <div className="flex items-center gap-2">
                <button
                  className={`btn btn-sm btn-outline ${isLoading ? 'btn-disabled' : ''}`}
                  type="button"
                  onClick={() => {
                    void handleReload()
                  }}
                  disabled={isLoading}
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
                  {isSaving ? 'Saving...' : 'Save Blueprint'}
                </button>
              </div>
            </div>

            {(loadError || saveError || saveSuccess) && (
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
            )}

            <label className="form-control w-full">
              <textarea
                  style={{height: 'calc(100vh - 340px)'}}
                className="textarea textarea-sm textarea-bordered  w-full font-mono text-xs"
                value={blueprintText}
                onChange={(event) => {
                  setBlueprintText(event.target.value)
                  setSaveError('')
                  setSaveSuccess('')
                }}
              />
            </label>
          </div>
        </div>
      </section>
    </>
  )
}
