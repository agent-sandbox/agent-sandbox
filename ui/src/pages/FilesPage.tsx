import { ChangeEvent, useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { deleteSandboxFile, downloadSandboxFile, listSandboxFiles, triggerBrowserDownload, uploadSandboxFile } from '../lib/api/files'
import { listSandboxes } from '../lib/api/sandbox'
import type { Sandbox, SandboxFileEntry } from '../lib/api/types'

function normalizePath(input: string): string {
  const trimmed = input.trim()
  if (!trimmed) {
    return '/'
  }

  const value = trimmed.startsWith('/') ? trimmed : `/${trimmed}`
  const segments = value.split('/').filter(Boolean)
  const normalized: string[] = []

  for (const segment of segments) {
    if (segment === '.') {
      continue
    }
    if (segment === '..') {
      normalized.pop()
      continue
    }
    normalized.push(segment)
  }

  return `/${normalized.join('/')}`.replace(/\/+/g, '/') || '/'
}

function parentPath(input: string): string {
  const current = normalizePath(input)
  if (current === '/') {
    return '/'
  }
  const parts = current.split('/').filter(Boolean)
  parts.pop()
  return parts.length > 0 ? `/${parts.join('/')}` : '/'
}

function formatFileSize(size: number): string {
  if (!Number.isFinite(size) || size < 0) {
    return '-'
  }
  if (size < 1024) {
    return `${size} B`
  }
  if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KB`
  }
  if (size < 1024 * 1024 * 1024) {
    return `${(size / (1024 * 1024)).toFixed(1)} MB`
  }
  return `${(size / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

function toPathSegments(input: string): Array<{ label: string; path: string }> {
  const normalized = normalizePath(input)
  if (normalized === '/') {
    return [{ label: '/', path: '/' }]
  }

  const segments = normalized.split('/').filter(Boolean)
  const result: Array<{ label: string; path: string }> = [{ label: '/', path: '/' }]

  let current = ''
  for (const segment of segments) {
    current += `/${segment}`
    result.push({ label: segment, path: current })
  }

  return result
}

export default function FilesPage() {
  const [searchParams, setSearchParams] = useSearchParams()

  const [sandboxes, setSandboxes] = useState<Sandbox[]>([])
  const [selectedSandboxName, setSelectedSandboxName] = useState('')
  const [currentPath, setCurrentPath] = useState('/')
  const [entries, setEntries] = useState<SandboxFileEntry[]>([])

  const [isSandboxesLoading, setIsSandboxesLoading] = useState(false)
  const [isFilesLoading, setIsFilesLoading] = useState(false)
  const [isUploading, setIsUploading] = useState(false)

  const [sandboxesError, setSandboxesError] = useState('')
  const [listError, setListError] = useState('')
  const [downloadError, setDownloadError] = useState('')
  const [downloadSuccess, setDownloadSuccess] = useState('')
  const [pendingDownloadEntry, setPendingDownloadEntry] = useState<SandboxFileEntry | null>(null)
  const [deleteError, setDeleteError] = useState('')
  const [deleteSuccess, setDeleteSuccess] = useState('')
  const [pendingDeleteEntry, setPendingDeleteEntry] = useState<SandboxFileEntry | null>(null)
  const [uploadError, setUploadError] = useState('')

  const selectedSandboxNameRef = useRef('')
  selectedSandboxNameRef.current = selectedSandboxName

  const currentPathRef = useRef('/')
  currentPathRef.current = currentPath

  const fileInputRef = useRef<HTMLInputElement | null>(null)

  const updateQuery = useCallback(
    (nextSandbox: string, nextPath: string) => {
      const params = new URLSearchParams(searchParams)

      if (nextSandbox) {
        params.set('sandbox', nextSandbox)
      } else {
        params.delete('sandbox')
      }

      if (nextPath && nextPath !== '/') {
        params.set('path', nextPath)
      } else {
        params.delete('path')
      }

      setSearchParams(params, { replace: true })
    },
    [searchParams, setSearchParams],
  )

  const loadFiles = useCallback(async (sandboxName: string, filePath: string, options?: { silent?: boolean }) => {
    const name = sandboxName.trim()
    if (!name) {
      return
    }

    const normalized = normalizePath(filePath)
    if (!options?.silent) {
      setIsFilesLoading(true)
    }
    setListError('')

    try {
      const data = await listSandboxFiles(name, normalized)
      if (selectedSandboxNameRef.current !== name || normalizePath(currentPathRef.current) !== normalized) {
        return
      }
      setEntries(Array.isArray(data.entries) ? data.entries : [])
      setCurrentPath(normalizePath(data.path || normalized))
    } catch (error) {
      if (selectedSandboxNameRef.current !== name || normalizePath(currentPathRef.current) !== normalized) {
        return
      }
      const message = error instanceof Error ? error.message : 'Failed to load files'
      setListError(message)
    } finally {
      if (!options?.silent) {
        setIsFilesLoading(false)
      }
    }
  }, [])

  const refreshSandboxes = useCallback(async () => {
    setIsSandboxesLoading(true)
    setSandboxesError('')

    try {
      const list = await listSandboxes()
      setSandboxes(list)

      if (list.length === 0) {
        setSelectedSandboxName('')
        setEntries([])
        return
      }

      const fromQuery = searchParams.get('sandbox')?.trim() ?? ''
      const hasQueryTarget = Boolean(fromQuery) && list.some((sandbox) => sandbox.name === fromQuery)
      const currentSelectionValid = Boolean(selectedSandboxNameRef.current) && list.some((sandbox) => sandbox.name === selectedSandboxNameRef.current)

      const nextSandbox = hasQueryTarget ? fromQuery : currentSelectionValid ? selectedSandboxNameRef.current : list[0]?.name ?? ''
      const nextPath = normalizePath(searchParams.get('path') ?? currentPathRef.current)

      setSelectedSandboxName(nextSandbox)
      setCurrentPath(nextPath)
      updateQuery(nextSandbox, nextPath)
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to load sandboxes'
      setSandboxesError(message)
      setSelectedSandboxName('')
      setEntries([])
    } finally {
      setIsSandboxesLoading(false)
    }
  }, [searchParams, updateQuery])

  useEffect(() => {
    void refreshSandboxes()
  }, [refreshSandboxes])

  useEffect(() => {
    if (!selectedSandboxName) {
      return
    }
    void loadFiles(selectedSandboxName, currentPath)
  }, [selectedSandboxName, currentPath, loadFiles])

  const handleSandboxChange = (event: ChangeEvent<HTMLSelectElement>) => {
    const nextSandbox = event.target.value
    const nextPath = normalizePath(currentPath)
    setSelectedSandboxName(nextSandbox)
    setDownloadError('')
    setDownloadSuccess('')
    setDeleteError('')
    setDeleteSuccess('')
    setUploadError('')
    updateQuery(nextSandbox, nextPath)
  }

  const handlePathInputChange = (event: ChangeEvent<HTMLInputElement>) => {
    setCurrentPath(event.target.value)
  }

  const handlePathSubmit = () => {
    const nextPath = normalizePath(currentPath)
    setCurrentPath(nextPath)
    updateQuery(selectedSandboxName, nextPath)
  }

  const handleGoParent = () => {
    const nextPath = parentPath(currentPath)
    setCurrentPath(nextPath)
    updateQuery(selectedSandboxName, nextPath)
  }

  const handleOpenEntry = (entry: SandboxFileEntry) => {
    if (!entry.isDir) {
      return
    }
    const nextPath = normalizePath(entry.path)
    setCurrentPath(nextPath)
    updateQuery(selectedSandboxName, nextPath)
  }

  const handlePathSegmentClick = (path: string) => {
    const nextPath = normalizePath(path)
    setCurrentPath(nextPath)
    updateQuery(selectedSandboxName, nextPath)
  }

  const pathSegments = toPathSegments(currentPath)

  const handleDownload = (entry: SandboxFileEntry) => {
    if (!selectedSandboxName || entry.isDir) {
      return
    }

    setPendingDownloadEntry(entry)
  }

  const handleConfirmDownload = async () => {
    if (!selectedSandboxName || !pendingDownloadEntry) {
      return
    }

    const entry = pendingDownloadEntry
    setPendingDownloadEntry(null)

    setDownloadError('')
    setDownloadSuccess(`Started downloading ${entry.name}`)
    try {
      const result = await downloadSandboxFile(selectedSandboxName, entry.path)
      const downloadedName = result.fileName || entry.name
      triggerBrowserDownload(downloadedName, result.blob)
    } catch (error) {
      setDownloadSuccess('')
      const message = error instanceof Error ? error.message : 'Failed to download file'
      setDownloadError(message)
    }
  }

  const handleCancelDownload = () => {
    setPendingDownloadEntry(null)
  }

  const handleDelete = (entry: SandboxFileEntry) => {
    if (!selectedSandboxName) {
      return
    }

    setPendingDeleteEntry(entry)
  }

  const handleConfirmDelete = async () => {
    if (!selectedSandboxName || !pendingDeleteEntry) {
      return
    }

    const entry = pendingDeleteEntry
    setPendingDeleteEntry(null)

    setDeleteError('')
    setDeleteSuccess('')
    try {
      await deleteSandboxFile(selectedSandboxName, entry.path)
      setDeleteSuccess(`Deleted ${entry.name}`)
      await loadFiles(selectedSandboxName, normalizePath(currentPath), { silent: true })
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to delete file'
      setDeleteError(message)
    }
  }

  const handleCancelDelete = () => {
    setPendingDeleteEntry(null)
  }

  const isDownloadModalOpen = Boolean(pendingDownloadEntry)
  const isDeleteModalOpen = Boolean(pendingDeleteEntry)

  const handlePickUpload = () => {
    fileInputRef.current?.click()
  }

  const handleUploadFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file || !selectedSandboxName) {
      return
    }

    setUploadError('')
    setIsUploading(true)
    try {
      await uploadSandboxFile(selectedSandboxName, normalizePath(currentPath), file)
      await loadFiles(selectedSandboxName, normalizePath(currentPath), { silent: true })
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to upload file'
      setUploadError(message)
    } finally {
      setIsUploading(false)
      event.target.value = ''
    }
  }

  return (
    <>
      <header className="card border border-base-300 bg-base-100 shadow-sm">
        <div className="card-body gap-3">
          <div>
            <h2 className="text-2xl font-semibold">Sandbox Files</h2>
            <p className="text-sm text-base-content/70">Browse directories, upload files, and download files from sandbox pods.</p>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <label className="flex items-center gap-2">
              <span className="text-sm">Sandbox</span>
              <select
                className="select select-sm select-bordered w-full max-w-xs"
                value={selectedSandboxName}
                onChange={handleSandboxChange}
                disabled={isSandboxesLoading || sandboxes.length === 0}
              >
                {sandboxes.length === 0 ? (
                  <option value="">No sandboxes</option>
                ) : (
                  sandboxes.map((sandbox, index) => {
                    const name = sandbox.name ?? ''
                    return (
                      <option key={name || sandbox.id || `files-sandbox-${index}`} value={name}>
                        {name || '-'}
                      </option>
                    )
                  })
                )}
              </select>
            </label>
              <button className="btn btn-sm btn-outline" type="button" onClick={() => void refreshSandboxes()} disabled={isSandboxesLoading}>
                  {isSandboxesLoading ? 'Loading...' : 'Reload Sandboxes'}
              </button>
          </div>
        </div>
      </header>

      {sandboxesError && (
        <section>
          <div className="alert alert-error justify-between">
            <span>{sandboxesError}</span>
            <button className="btn btn-ghost btn-xs" type="button" onClick={() => setSandboxesError('')}>
              Close
            </button>
          </div>
        </section>
      )}

      {listError && (
        <section>
          <div className="alert alert-error justify-between">
            <span>{listError}</span>
            <button className="btn btn-ghost btn-xs" type="button" onClick={() => setListError('')}>
              Close
            </button>
          </div>
        </section>
      )}
      {downloadError && (
        <section>
          <div className="alert alert-error justify-between">
            <span>{downloadError}</span>
            <button className="btn btn-ghost btn-xs" type="button" onClick={() => setDownloadError('')}>
              Close
            </button>
          </div>
        </section>
      )}

      {downloadSuccess && (
        <section>
          <div className="alert alert-success justify-between">
            <span>{downloadSuccess}</span>
            <button className="btn btn-ghost btn-xs" type="button" onClick={() => setDownloadSuccess('')}>
              Close
            </button>
          </div>
        </section>
      )}

      {deleteError && (
        <section>
          <div className="alert alert-error justify-between">
            <span>{deleteError}</span>
            <button className="btn btn-ghost btn-xs" type="button" onClick={() => setDeleteError('')}>
              Close
            </button>
          </div>
        </section>
      )}

      {deleteSuccess && (
        <section>
          <div className="alert alert-success justify-between">
            <span>{deleteSuccess}</span>
            <button className="btn btn-ghost btn-xs" type="button" onClick={() => setDeleteSuccess('')}>
              Close
            </button>
          </div>
        </section>
      )}

      {uploadError && (
        <section>
          <div className="alert alert-error justify-between">
            <span>{uploadError}</span>
            <button className="btn btn-ghost btn-xs" type="button" onClick={() => setUploadError('')}>
              Close
            </button>
          </div>
        </section>
      )}

      <section>
        <div className="card border border-base-300 bg-base-100 shadow-sm">
            <div className="card-body gap-3">
                <div className="flex flex-wrap gap-2 " style={{marginLeft: 'auto'}}>
                    <label className="flex items-center gap-2">
                        <span className="text-sm">Path</span>
                        <input className="input input-sm input-bordered w-72" value={currentPath}
                               onChange={handlePathInputChange} placeholder="/"/>
                    </label>

                    <button className="btn btn-sm btn-outline" type="button" onClick={handlePathSubmit}
                            disabled={!selectedSandboxName || isFilesLoading}>
                        Go
                    </button>

                    <button className="btn btn-sm btn-outline" type="button" onClick={handleGoParent}
                            disabled={!selectedSandboxName || isFilesLoading || normalizePath(currentPath) === '/'}>
                        Up
                    </button>

                    <button className="btn btn-sm btn-outline" type="button"
                            onClick={() => void loadFiles(selectedSandboxName, normalizePath(currentPath))}
                            disabled={!selectedSandboxName || isFilesLoading}>
                        {isFilesLoading ? 'Refreshing...' : 'Refresh'}
                    </button>


                    <input ref={fileInputRef} type="file" className="hidden" onChange={handleUploadFile}/>
                    <button className="btn btn-sm btn-primary" type="button" onClick={handlePickUpload}
                            disabled={!selectedSandboxName || isUploading}>
                        {isUploading ? 'Uploading...' : 'Upload File'}
                    </button>
                </div>

                    <h3 className="card-title text-lg flex-wrap gap-2">
                        <span>Files -</span>
                        <span className="text-sm font-normal text-base-content/70 flex flex-wrap items-center gap-1">
                {pathSegments.map((segment, index) => (
                    <span key={`${segment.path}-${index}`} className="inline-flex items-center gap-1">
                    {index > 0 && <span className="opacity-60">/</span>}
                        <button className="btn btn-link btn-xs px-0 min-h-0 h-auto normal-case" type="button"
                                onClick={() => handlePathSegmentClick(segment.path)}>
                      {segment.label}
                    </button>
                  </span>
                ))}
              </span>
                    </h3>

                    {!selectedSandboxName ? (
                        <p className="text-sm text-base-content/60">Please select a sandbox.</p>
                    ) : entries.length === 0 ? (
                        <p className="text-sm text-base-content/60">No files found.</p>
                    ) : (
                        <div className="h-[calc(100vh-25rem)] overflow-x-auto rounded-box border border-base-300">
                            <table className="table table-sm table-pin-rows table-zebra">
                                <thead>
                                <tr>
                                    <th>Name</th>
                                    <th>Type</th>
                                    <th>Size</th>
                                    <th className="text-right">Actions</th>
                                </tr>
                                </thead>
                                <tbody>
                                {entries.map((entry) => (
                                    <tr key={`${entry.path}-${entry.isDir ? 'd' : 'f'}`}>
                                        <td>
                                            {entry.isDir ? (
                                                <button className="btn btn-link btn-xs px-0 normal-case" type="button"
                                                        onClick={() => handleOpenEntry(entry)}>
                                                    {entry.name}
                                                </button>
                                            ) : (
                                                <span>{entry.name}</span>
                                            )}
                                        </td>
                                        <td>{entry.isDir ? 'Directory' : 'File'}</td>
                                        <td>{entry.isDir ? '-' : formatFileSize(entry.size)}</td>
                                        <td className="text-right">
                                            <div className="inline-flex items-center gap-2">
                                                {!entry.isDir && (
                                                    <button className="btn btn-xs btn-outline" type="button"
                                                            onClick={() => void handleDownload(entry)}>
                                                        Download
                                                    </button>
                                                )}
                                                {!entry.isDir && (
                                                    <button className="btn btn-xs btn-error btn-outline" type="button"
                                                            onClick={() => handleDelete(entry)}>
                                                        Delete
                                                    </button>
                                                )}
                                            </div>
                                        </td>
                                    </tr>
                                ))}
                                </tbody>
                            </table>
                        </div>
                    )}
            </div>
            </div>
      </section>

        <dialog className={`modal ${isDownloadModalOpen ? 'modal-open' : ''}`}>
        <div className="modal-box">
          <h3 className="font-semibold text-lg">Confirm Download</h3>
          <p className="py-4">Download file: {pendingDownloadEntry?.name ?? ''}?</p>
          <div className="modal-action">
            <button className="btn btn-sm" type="button" onClick={handleCancelDownload}>
              Cancel
            </button>
            <button className="btn btn-sm btn-primary" type="button" onClick={() => void handleConfirmDownload()}>
              Download
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button type="button" onClick={handleCancelDownload}>close</button>
        </form>
      </dialog>

      <dialog className={`modal ${isDeleteModalOpen ? 'modal-open' : ''}`}>
        <div className="modal-box">
          <h3 className="font-semibold text-lg">Confirm Delete</h3>
          <p className="py-4">Delete file: {pendingDeleteEntry?.name ?? ''}?</p>
          <div className="modal-action">
            <button className="btn btn-sm" type="button" onClick={handleCancelDelete}>
              Cancel
            </button>
            <button className="btn btn-sm btn-error" type="button" onClick={() => void handleConfirmDelete()}>
              Delete
            </button>

          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button type="button" onClick={handleCancelDelete}>close</button>
        </form>
      </dialog>
    </>
  )
}
