import { useEffect, useState } from 'react'

import { getTemplatesConfig, saveTemplatesConfig } from '../lib/api/config'
import type { Template, TemplateResources } from '../lib/api/types'

type EditableTemplateResources = {
  cpu?: string
  memory?: string
  cpuLimit?: string
  memoryLimit?: string
}

type EditableTemplatePool = {
  size?: number | string
  readySize?: number | string
  probePort?: number | string
  warmupCmd?: string
  startupCmd?: string
  resources?: EditableTemplateResources
}

type EditableTemplate = Omit<Template, 'port' | 'pool' | 'resources'> & {
  port?: number | string
  resources?: EditableTemplateResources
  pool?: EditableTemplatePool
}

function parseOptionalInteger(value: unknown, fieldName: string): number | undefined {
  if (value === undefined || value === null || value === '') {
    return undefined
  }

  if (typeof value === 'number') {
    if (Number.isInteger(value)) {
      return value
    }
    throw new Error(`${fieldName} must be an integer.`)
  }

  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (!trimmed) {
      return undefined
    }

    const parsed = Number.parseInt(trimmed, 10)
    if (!Number.isNaN(parsed)) {
      return parsed
    }
  }

  throw new Error(`${fieldName} must be an integer.`)
}

function parseMetadata(metadata: unknown): Record<string, string> | undefined {
  if (!metadata || typeof metadata !== 'object' || Array.isArray(metadata)) {
    return undefined
  }

  const parsed: Record<string, string> = {}
  for (const [key, value] of Object.entries(metadata)) {
    const keyText = key.trim()
    if (!keyText) {
      continue
    }
    parsed[keyText] = String(value ?? '').trim()
  }

  return Object.keys(parsed).length > 0 ? parsed : undefined
}

function metadataToText(metadata: unknown): string {
  const parsed = parseMetadata(metadata)
  if (!parsed) {
    return ''
  }

  return Object.entries(parsed)
    .map(([key, value]) => `${key}=${value}`)
    .join('\n')
}

function metadataTextToObject(value: string): Record<string, string> | undefined {
  const lines = value.split('\n')
  const metadata: Record<string, string> = {}

  for (let index = 0; index < lines.length; index++) {
    const line = lines[index]?.trim() ?? ''
    if (!line) {
      continue
    }

    const delimiterIndex = line.indexOf('=')
    if (delimiterIndex <= 0) {
      throw new Error(`Template metadata line #${index + 1} must be key=value.`)
    }

    const key = line.slice(0, delimiterIndex).trim()
    const val = line.slice(delimiterIndex + 1).trim()
    if (!key) {
      throw new Error(`Template metadata line #${index + 1} has an empty key.`)
    }

    metadata[key] = val
  }

  return Object.keys(metadata).length > 0 ? metadata : undefined
}

function parseResources(resources: unknown): TemplateResources | undefined {
  if (!resources || typeof resources !== 'object' || Array.isArray(resources)) {
    return undefined
  }

  const value = resources as EditableTemplateResources
  const parsed: TemplateResources = {
    cpu: value.cpu?.trim() || undefined,
    memory: value.memory?.trim() || undefined,
    cpuLimit: value.cpuLimit?.trim() || undefined,
    memoryLimit: value.memoryLimit?.trim() || undefined,
  }

  return Object.values(parsed).some((item) => item) ? parsed : undefined
}

function getCPUFormatHint(value: string): string | undefined {
  const trimmed = value.trim()
  if (!trimmed) {
    return undefined
  }

  if (!/^(\d+m|\d+(\.\d+)?)$/.test(trimmed)) {
    return 'Suggested format: 100m or 1'
  }

  return undefined
}

function getMemoryFormatHint(value: string): string | undefined {
  const trimmed = value.trim()
  if (!trimmed) {
    return undefined
  }

  if (!/^(\d+(\.\d+)?)(Ki|Mi|Gi|Ti|Pi|Ei|K|M|G|T|P|E)?$/.test(trimmed)) {
    return 'Suggested format: 128Mi or 1Gi'
  }

  if (/^\d+(\.\d+)?$/.test(trimmed)) {
    return 'Consider adding unit, e.g. Mi or Gi'
  }

  return undefined
}

function toSaveTemplatesPayload(templates: EditableTemplate[]): Template[] {
  if (!Array.isArray(templates)) {
    throw new Error('Templates must be an array.')
  }

  return templates.map((template, index) => {
    const pool = template.pool

    return {
      name: template.name?.trim() || undefined,
      description: template.description?.trim() || undefined,
      image: template.image?.trim() || undefined,
      type: template.type?.trim() || undefined,
      pattern: template.pattern?.trim() || undefined,
      metadata: parseMetadata(template.metadata),
      args: Array.isArray(template.args) && template.args.length > 0 ? template.args : undefined,
      extraPorts: Array.isArray(template.extraPorts) && template.extraPorts.length > 0 ? template.extraPorts : undefined,
      envVars: parseMetadata(template.envVars),
      shell: template.shell?.trim() || undefined,
      noStartupProbe: Boolean(template.noStartupProbe),
      port: parseOptionalInteger(template.port, `Template #${index + 1} port`),
      resources: parseResources(template.resources),
      pool: {
        size: parseOptionalInteger(pool?.size, `Template #${index + 1} pool.size`),
        readySize: parseOptionalInteger(pool?.readySize, `Template #${index + 1} pool.readySize`),
        probePort: parseOptionalInteger(pool?.probePort, `Template #${index + 1} pool.probePort`),
        warmupCmd: pool?.warmupCmd?.trim() || undefined,
        startupCmd: pool?.startupCmd?.trim() || undefined,
        resources: parseResources(pool?.resources),
      },
    }
  })
}

function ResourcesEditor(props: {
  title: string
  resources?: EditableTemplateResources
  onChange: (field: keyof EditableTemplateResources, value: string) => void
}) {
  const { title, resources, onChange } = props

  const cpu = resources?.cpu ?? ''
  const memory = resources?.memory ?? ''
  const cpuLimit = resources?.cpuLimit ?? ''
  const memoryLimit = resources?.memoryLimit ?? ''

  return (
    <div className="rounded-box border border-base-300 bg-base-200/40 p-3 md:col-span-2">
      <div className="mb-2 text-sm font-medium">{title}</div>
      <div className="grid gap-3 md:grid-cols-2">
        <label className="form-control w-full">
          <div className="label">
            <span className="label-text">CPU</span>
          </div>
          <input className="input input-sm input-bordered w-full" type="text" placeholder="100m" value={cpu} onChange={(event) => onChange('cpu', event.target.value)} />
          {getCPUFormatHint(cpu) && (
            <div className="label py-1">
              <span className="label-text-alt text-warning">{getCPUFormatHint(cpu)}</span>
            </div>
          )}
        </label>

        <label className="form-control w-full">
          <div className="label">
            <span className="label-text">Memory</span>
          </div>
          <input className="input input-sm input-bordered w-full" type="text" placeholder="128Mi" value={memory} onChange={(event) => onChange('memory', event.target.value)} />
          {getMemoryFormatHint(memory) && (
            <div className="label py-1">
              <span className="label-text-alt text-warning">{getMemoryFormatHint(memory)}</span>
            </div>
          )}
        </label>

        <label className="form-control w-full">
          <div className="label">
            <span className="label-text">CPU Limit</span>
          </div>
          <input className="input input-sm input-bordered w-full" type="text" placeholder="1" value={cpuLimit} onChange={(event) => onChange('cpuLimit', event.target.value)} />
          {getCPUFormatHint(cpuLimit) && (
            <div className="label py-1">
              <span className="label-text-alt text-warning">{getCPUFormatHint(cpuLimit)}</span>
            </div>
          )}
        </label>

        <label className="form-control w-full">
          <div className="label">
            <span className="label-text">Memory Limit</span>
          </div>
          <input className="input input-sm input-bordered w-full" type="text" placeholder="1Gi" value={memoryLimit} onChange={(event) => onChange('memoryLimit', event.target.value)} />
          {getMemoryFormatHint(memoryLimit) && (
            <div className="label py-1">
              <span className="label-text-alt text-warning">{getMemoryFormatHint(memoryLimit)}</span>
            </div>
          )}
        </label>
      </div>
    </div>
  )
}

export default function TemplatesConfigPage() {
  const [templates, setTemplates] = useState<EditableTemplate[]>([])
  const [selectedTemplateIndex, setSelectedTemplateIndex] = useState<number | null>(null)
  const [editorMode, setEditorMode] = useState<'form' | 'raw'>('form')
  const [rawTemplatesText, setRawTemplatesText] = useState('[]')
  const [isTemplatesLoading, setIsTemplatesLoading] = useState(false)
  const [isTemplatesSaving, setIsTemplatesSaving] = useState(false)
  const [templatesLoadError, setTemplatesLoadError] = useState('')
  const [templatesParseError, setTemplatesParseError] = useState('')
  const [templatesSaveError, setTemplatesSaveError] = useState('')
  const [templatesSaveSuccess, setTemplatesSaveSuccess] = useState('')
  const [metadataDraft, setMetadataDraft] = useState('')
  const [envVarsDraft, setEnvVarsDraft] = useState('')
  const [argsDraft, setArgsDraft] = useState('')
  const [extraPortsDraft, setExtraPortsDraft] = useState('')

  const loadTemplates = async (options?: { keepMessages?: boolean }) => {
    setIsTemplatesLoading(true)
    setTemplatesLoadError('')
    setTemplatesParseError('')

    if (!options?.keepMessages) {
      setTemplatesSaveError('')
      setTemplatesSaveSuccess('')
    }

    try {
      const data = await getTemplatesConfig()
      const raw = typeof data === 'string' ? data.trim() : ''

      if (!raw) {
        setTemplates([])
        setRawTemplatesText('[]')
        setSelectedTemplateIndex(null)
        setMetadataDraft('')
        setEnvVarsDraft('')
        setArgsDraft('')
        setExtraPortsDraft('')
        return
      }

      let parsed: unknown
      try {
        parsed = JSON.parse(raw)
      } catch {
        throw new Error('Failed to parse templates config JSON returned by backend.')
      }

      if (!Array.isArray(parsed)) {
        throw new Error('Templates config JSON must be an array.')
      }

      const nextTemplates = parsed as EditableTemplate[]
      setTemplates(nextTemplates)
      setRawTemplatesText(JSON.stringify(nextTemplates, null, 2))
      setSelectedTemplateIndex((prev) => {
        if (nextTemplates.length === 0) {
          setMetadataDraft('')
          setEnvVarsDraft('')
          setArgsDraft('')
          setExtraPortsDraft('')
          return null
        }
        const nextIndex = prev === null || prev >= nextTemplates.length ? 0 : prev
        setMetadataDraft(metadataToText(nextTemplates[nextIndex]?.metadata))
        setEnvVarsDraft(metadataToText(nextTemplates[nextIndex]?.envVars))
        setArgsDraft((nextTemplates[nextIndex]?.args ?? []).join('\n'))
        setExtraPortsDraft((nextTemplates[nextIndex]?.extraPorts ?? []).join(', '))
        return nextIndex
      })
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to load templates config'
      if (message.includes('parse') || message.includes('array')) {
        setTemplatesParseError(message)
      } else {
        setTemplatesLoadError(message)
      }
      setTemplates([])
      setRawTemplatesText('[]')
      setSelectedTemplateIndex(null)
      setMetadataDraft('')
      setEnvVarsDraft('')
      setArgsDraft('')
      setExtraPortsDraft('')
    } finally {
      setIsTemplatesLoading(false)
    }
  }

  useEffect(() => {
    void loadTemplates()
  }, [])

  const handleReloadTemplates = async () => {
    await loadTemplates({ keepMessages: true })
  }

  const clearSaveMessages = () => {
    setTemplatesSaveError('')
    setTemplatesSaveSuccess('')
  }

  const handleSelectTemplate = (index: number) => {
    setSelectedTemplateIndex(index)
    setMetadataDraft(metadataToText(templates[index]?.metadata))
    setEnvVarsDraft(metadataToText(templates[index]?.envVars))
    setArgsDraft((templates[index]?.args ?? []).join('\n'))
    setExtraPortsDraft((templates[index]?.extraPorts ?? []).join(', '))
    setTemplatesParseError('')
    clearSaveMessages()
  }

  const updateSelectedTemplate = (updater: (template: EditableTemplate) => EditableTemplate) => {
    setTemplates((previous) => {
      if (selectedTemplateIndex === null || selectedTemplateIndex < 0 || selectedTemplateIndex >= previous.length) {
        return previous
      }

      const next = [...previous]
      next[selectedTemplateIndex] = updater(next[selectedTemplateIndex] ?? {})
      setRawTemplatesText(JSON.stringify(next, null, 2))
      return next
    })
  }

  const handleSwitchToRawMode = () => {
    setRawTemplatesText(JSON.stringify(templates, null, 2))
    setTemplatesParseError('')
    clearSaveMessages()
    setEditorMode('raw')
  }

  const handleSwitchToFormMode = () => {
    try {
      const parsed = JSON.parse(rawTemplatesText)
      if (!Array.isArray(parsed)) {
        throw new Error('Raw JSON must be an array.')
      }

      const nextTemplates = parsed as EditableTemplate[]
      setTemplates(nextTemplates)
      setSelectedTemplateIndex(nextTemplates.length === 0 ? null : 0)
      setMetadataDraft(nextTemplates.length === 0 ? '' : metadataToText(nextTemplates[0]?.metadata))
      setEnvVarsDraft(nextTemplates.length === 0 ? '' : metadataToText(nextTemplates[0]?.envVars))
      setArgsDraft(nextTemplates.length === 0 ? '' : (nextTemplates[0]?.args ?? []).join('\n'))
      setExtraPortsDraft(nextTemplates.length === 0 ? '' : (nextTemplates[0]?.extraPorts ?? []).join(', '))
      setTemplatesParseError('')
      clearSaveMessages()
      setEditorMode('form')
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to parse raw templates JSON'
      setTemplatesParseError(message)
    }
  }

  const handleSaveTemplates = async () => {
    setIsTemplatesSaving(true)
    setTemplatesSaveError('')
    setTemplatesSaveSuccess('')

    try {
      const sourceTemplates =
        editorMode === 'raw'
          ? (() => {
              let parsed: unknown
              try {
                parsed = JSON.parse(rawTemplatesText)
              } catch {
                throw new Error('Raw JSON is invalid and cannot be saved.')
              }

              if (!Array.isArray(parsed)) {
                throw new Error('Raw JSON must be an array.')
              }

              return parsed as EditableTemplate[]
            })()
          : templates

      const payload = toSaveTemplatesPayload(sourceTemplates)
      await saveTemplatesConfig(payload)
      setTemplatesSaveSuccess('Templates saved successfully.')
      await loadTemplates({ keepMessages: true })
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to save templates config'
      setTemplatesSaveError(message)
    } finally {
      setIsTemplatesSaving(false)
    }
  }

  const selectedTemplate = selectedTemplateIndex === null ? undefined : templates[selectedTemplateIndex]
  const isRawMode = editorMode === 'raw'

  return (
    <>
      <header className="card border border-base-300 bg-base-100 shadow-sm">
        <div className="card-body gap-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-2xl font-semibold">Templates Config</h2>
              <p className="text-sm text-base-content/70">View, edit, and save templates configuration.</p>
            </div>
          </div>
        </div>
      </header>

      <section id="templates-config">
        <div className="card border border-base-300 bg-base-100 shadow-sm">
          <div className="card-body gap-4">
            <div className="flex items-center justify-between gap-2">
              <h3 className="card-title text-lg">Templates Config</h3>
              <div className="flex items-center gap-2">
                <button className={`btn btn-sm btn-dash ${!isRawMode ? 'btn-warning' : 'btn-success'}`} type="button" onClick={handleSwitchToFormMode} disabled={!isRawMode}>
                  Form
                </button>
                <button className={`btn btn-sm btn-dash ${isRawMode ? 'btn-success' : 'btn-warning'}`} type="button" onClick={handleSwitchToRawMode} disabled={isRawMode}>
                  Raw
                </button>
                <div className="divider lg:divider-horizontal"></div>
                <button className={`btn btn-sm btn-outline ${isTemplatesLoading ? 'btn-disabled' : ''}`} type="button" onClick={() => void handleReloadTemplates()} disabled={isTemplatesLoading}>
                  {isTemplatesLoading ? 'Reloading...' : 'Reload'}
                </button>
                <button className={`btn btn-sm btn-primary ${isTemplatesSaving ? 'btn-disabled' : ''}`} type="button" onClick={() => void handleSaveTemplates()} disabled={isTemplatesSaving || isTemplatesLoading}>
                  {isTemplatesSaving ? 'Saving...' : 'Save Templates'}
                </button>
              </div>
            </div>

            {(templatesLoadError || templatesParseError || templatesSaveError || templatesSaveSuccess) && (
              <div className="space-y-2">
                {templatesLoadError && (
                  <div className="alert alert-error">
                    <span>{templatesLoadError}</span>
                  </div>
                )}
                {templatesParseError && (
                  <div className="alert alert-error">
                    <span>{templatesParseError}</span>
                  </div>
                )}
                {templatesSaveError && (
                  <div className="alert alert-error">
                    <span>{templatesSaveError}</span>
                  </div>
                )}
                {templatesSaveSuccess && (
                  <div className="alert alert-success">
                    <span>{templatesSaveSuccess}</span>
                  </div>
                )}
              </div>
            )}

            {isRawMode ? (
              <label className="form-control w-full">
                <div className="label">
                  <span className="label-text">Templates JSON</span>
                </div>
                <textarea
                  className="textarea textarea-sm textarea-bordered min-h-[650px] w-full font-mono text-xs"
                  value={rawTemplatesText}
                  onChange={(event) => {
                    setRawTemplatesText(event.target.value)
                    setTemplatesParseError('')
                    clearSaveMessages()
                  }}
                />
              </label>
            ) : (
              <div className="grid gap-4 lg:grid-cols-[300px,1fr]">
                <div className="rounded-box border border-base-300 p-2">
                  <div className="mb-2 text-sm font-medium">Templates</div>
                  <div className="custom-scrollbar max-h-[420px] overflow-auto">
                    {templates.length === 0 ? (
                      <div className="px-2 py-3 text-sm text-base-content/70">{isTemplatesLoading ? 'Loading templates...' : 'No templates found.'}</div>
                    ) : (
                      <div className="menu w-full gap-1 p-0">
                        {templates.map((template, index) => {
                          const label = template.name?.trim() || `Template ${index + 1}`
                          const isSelected = index === selectedTemplateIndex

                          return (
                            <button key={`${label}-${index}`} type="button" className={`btn btn-sm justify-start ${isSelected ? 'btn-primary' : 'btn-ghost'}`} onClick={() => handleSelectTemplate(index)}>
                              {label}
                            </button>
                          )
                        })}
                      </div>
                    )}
                  </div>
                </div>

                <div className="rounded-box border border-base-300 p-4">
                  {!selectedTemplate ? (
                    <div className="text-sm text-base-content/70">Select a template from the list to edit.</div>
                  ) : (
                    <div className="space-y-3">
                      <div className="grid gap-3 md:grid-cols-2">
                        <label className="form-control w-full">
                          <div className="label">
                            <span className="label-text">Name *</span>
                          </div>
                          <input className="input input-sm input-bordered w-full" type="text" value={selectedTemplate.name ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, name: event.target.value }))} />
                        </label>



                          <label className="form-control w-full">
                              <div className="label">
                                  <span className="label-text">Port</span>
                              </div>
                              <input className="input input-sm input-bordered w-full" type="text" value={selectedTemplate.port ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, port: event.target.value }))} />
                          </label>

                        <label className="form-control w-full md:col-span-2">
                          <div className="label">
                            <span className="label-text">Description</span>
                          </div>
                          <input className="input input-sm input-bordered w-full" type="text" value={selectedTemplate.description ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, description: event.target.value }))} />
                        </label>

                        <label className="form-control w-full md:col-span-2">
                          <div className="label">
                            <span className="label-text">Image *</span>
                          </div>
                          <input className="input input-sm input-bordered w-full" type="text" value={selectedTemplate.image ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, image: event.target.value }))} />
                        </label>

                        <label className="form-control w-full">
                          <div className="label">
                            <span className="label-text">Type (normal/dynamic)</span>
                          </div>
                          <input className="input input-sm input-bordered w-full" type="text" value={selectedTemplate.type ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, type: event.target.value }))} />
                        </label>

                          <label className="form-control w-full">
                              <div className="label">
                                  <span className="label-text">Pattern (requeried if type is dynamic)</span>
                              </div>
                              <input className="input input-sm input-bordered w-full" type="text" value={selectedTemplate.pattern ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, pattern: event.target.value }))} />
                          </label>

                        <label className="form-control w-full">
                          <div className="label mr-1">
                            <span className="label-text">No Startup Probe</span>
                          </div>
                          <input className="checkbox checkbox-sm" type="checkbox" checked={Boolean(selectedTemplate.noStartupProbe)} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, noStartupProbe: event.target.checked }))} />
                        </label>

                        <label className="form-control w-full md:col-span-2">
                          <div className="label">
                            <span className="label-text">Args (one per line)</span>
                          </div>
                          <textarea
                            className="textarea textarea-sm textarea-bordered w-full font-mono text-xs"
                            value={argsDraft}
                            onChange={(event) => {
                              const value = event.target.value
                              setArgsDraft(value)
                              const args = value.split('\n').map((s) => s.trim()).filter(Boolean)
                              updateSelectedTemplate((prev) => ({ ...prev, args: args.length > 0 ? args : undefined }))
                            }}
                          />
                        </label>

                        <label className="form-control w-full md:col-span-2">
                          <div className="label">
                            <span className="label-text">Shell (terminal shell path, e.g. /bin/bash, default: sh)</span>
                          </div>
                          <input className="input input-sm input-bordered w-full font-mono" type="text" value={selectedTemplate.shell ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, shell: event.target.value || undefined }))} />
                        </label>

                        <label className="form-control w-full md:col-span-2">
                          <div className="label">
                            <span className="label-text">Extra Ports (comma-separated, e.g. 6080, 9090)</span>
                          </div>
                          <input
                            className="input input-sm input-bordered w-full font-mono"
                            type="text"
                            value={extraPortsDraft}
                            onChange={(event) => {
                              const value = event.target.value
                              setExtraPortsDraft(value)
                              const ports = value.split(',').map((s) => Number.parseInt(s.trim(), 10)).filter((n) => Number.isInteger(n) && n > 0 && n <= 65535)
                              updateSelectedTemplate((prev) => ({ ...prev, extraPorts: ports.length > 0 ? ports : undefined }))
                            }}
                          />
                        </label>

                        <label className="form-control w-full md:col-span-2">
                          <div className="label">
                            <span className="label-text">Env Vars (key=value, one per line)</span>
                          </div>
                          <textarea
                            className="textarea textarea-sm textarea-bordered w-full font-mono text-xs"
                            value={envVarsDraft}
                            onChange={(event) => {
                              const value = event.target.value
                              setEnvVarsDraft(value)
                              try {
                                const envVars = metadataTextToObject(value)
                                updateSelectedTemplate((prev) => ({ ...prev, envVars }))
                                setTemplatesParseError('')
                              } catch (error) {
                                const message = error instanceof Error ? error.message : 'Invalid env vars format.'
                                setTemplatesParseError(message)
                              }
                            }}
                          />
                        </label>

                        <label className="form-control w-full md:col-span-2">
                          <div className="label">
                            <span className="label-text">Metadata (key=value, one per line, e.g. runtimeClassName=gvisor, no quotation marks)</span>
                          </div>
                          <textarea
                            className="textarea textarea-sm textarea-bordered min-h-[120px] w-full font-mono text-xs"
                            value={metadataDraft}
                            onChange={(event) => {
                              const value = event.target.value
                              setMetadataDraft(value)
                              try {
                                const metadata = metadataTextToObject(value)
                                updateSelectedTemplate((prev) => ({ ...prev, metadata }))
                                setTemplatesParseError('')
                              } catch (error) {
                                const message = error instanceof Error ? error.message : 'Invalid metadata format.'
                                setTemplatesParseError(message)
                              }
                            }}
                          />
                        </label>

                        <div className="md:col-span-2 text-xs text-base-content/70">Only non-empty key=value lines are saved.</div>


                        <ResourcesEditor
                          title="Resources"
                          resources={selectedTemplate.resources}
                          onChange={(field, value) => {
                            updateSelectedTemplate((prev) => ({
                              ...prev,
                              resources: {
                                ...prev.resources,
                                [field]: value,
                              },
                            }))
                          }}
                        />

                          <div className="divider md:col-span-2 my-0"></div>

                        <div className="md:col-span-2 text-sm font-medium">Template Pool Settings</div>

                        <ResourcesEditor
                          title="Pool Resources"
                          resources={selectedTemplate.pool?.resources}
                          onChange={(field, value) => {
                            updateSelectedTemplate((prev) => ({
                              ...prev,
                              pool: {
                                ...prev.pool,
                                resources: {
                                  ...prev.pool?.resources,
                                  [field]: value,
                                },
                              },
                            }))
                          }}
                        />

                        <label className="form-control w-full">
                          <div className="label">
                            <span className="label-text">Pool Ready Size</span>
                          </div>
                          <input className="input input-sm input-bordered w-full" type="text" value={selectedTemplate.pool?.readySize ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, pool: { ...prev.pool, readySize: event.target.value } }))} />
                        </label>

                        <label className="form-control w-full">
                          <div className="label">
                            <span className="label-text">Pool Size</span>
                          </div>
                          <input className="input input-sm input-bordered w-full" type="text" value={selectedTemplate.pool?.size ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, pool: { ...prev.pool, size: event.target.value } }))} />
                        </label>

                        <label className="form-control w-full">
                          <div className="label">
                            <span className="label-text">Pool Probe Port</span>
                          </div>
                          <input className="input input-sm input-bordered w-full" type="text" value={selectedTemplate.pool?.probePort ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, pool: { ...prev.pool, probePort: event.target.value } }))} />
                        </label>

                        <label className="form-control w-full md:col-span-2">
                          <div className="label">
                            <span className="label-text">Pool Warmup Command</span>
                          </div>
                          <input className="input input-sm input-bordered w-full" type="text" value={selectedTemplate.pool?.warmupCmd ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, pool: { ...prev.pool, warmupCmd: event.target.value } }))} />
                        </label>

                        <label className="form-control w-full md:col-span-2">
                          <div className="label">
                            <span className="label-text">Pool Startup Command</span>
                          </div>
                          <input className="input input-sm input-bordered w-full" type="text" value={selectedTemplate.pool?.startupCmd ?? ''} onChange={(event) => updateSelectedTemplate((prev) => ({ ...prev, pool: { ...prev.pool, startupCmd: event.target.value } }))} />
                        </label>
                      </div>
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>
        </div>
      </section>
    </>
  )
}
