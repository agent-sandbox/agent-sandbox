import { FormEvent, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { listSandboxes } from '../lib/api/sandbox'
import { clearAuthToken, setAuthToken } from '../lib/auth/token'

export default function LoginPage() {
  const navigate = useNavigate()
  const [token, setToken] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')

  const isSubmitDisabled = useMemo(() => isSubmitting || token.trim() === '', [isSubmitting, token])

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const trimmed = token.trim()
    if (!trimmed) {
      setErrorMessage('Please enter an API token.')
      return
    }

    setIsSubmitting(true)
    setErrorMessage('')

    try {
      setAuthToken(trimmed)
      await listSandboxes()
      navigate('/sandboxes', { replace: true })
    } catch (error) {
      clearAuthToken()
      setErrorMessage(error instanceof Error ? error.message : 'Login failed')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="min-h-screen bg-base-200 p-4 lg:p-6">
      <div className="mx-auto mt-16 w-full max-w-md rounded-box border border-base-300 bg-base-100 p-8 sm:p-20 shadow-sm">
        <div className="mb-6 text-center">
          <h1 className="text-xl font-semibold">Agent Sandbox</h1>
          <p className="m-2">Dashboard</p>
          <p className="mt-5 text-sm text-base-content/70">Enter your API Token to continue.</p>
        </div>

        <form className="space-y-4" onSubmit={(event) => void handleSubmit(event)}>
          <div className="form-control">
            <div>
                <label className="label" htmlFor="api-token">
                    <span className="label-text">API Token :</span>
                </label>
            </div>
            <div className="mt-3 mb-10">
                    <input
                    id="api-token"
                    type="password"
                    className="input input-bordered input-sm w-full"
                    autoComplete="off"
                    value={token}
                    onChange={(event) => setToken(event.target.value)}
                    placeholder="Enter API token"
                    disabled={isSubmitting}
                />
            </div>
          </div>

          {errorMessage && <div className="alert alert-error text-sm py-2">{errorMessage}</div>}

          <button type="submit" className="btn btn-primary btn-sm w-full" disabled={isSubmitDisabled}>
            {isSubmitting ? 'Verifying...' : 'Login'}
          </button>
        </form>
      </div>
    </div>
  )
}
