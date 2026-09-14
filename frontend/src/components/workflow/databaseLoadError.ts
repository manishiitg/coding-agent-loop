type HTTPErrorShape = {
  message?: unknown
  response?: {
    data?: {
      error?: unknown
      message?: unknown
    }
  }
}

export type DatabaseLoadFailure = {
  missing: boolean
  message: string
}

// A managed database is created lazily, when the project first needs durable
// data. The workspace service reports that normal pre-creation state as a 400;
// keep the transport detail out of the product UI while preserving real errors.
export function describeDatabaseLoadFailure(error: unknown): DatabaseLoadFailure {
  const shaped = error && typeof error === 'object' ? error as HTTPErrorShape : null
  const responseError = typeof shaped?.response?.data?.error === 'string'
    ? shaped.response.data.error.trim()
    : ''
  const responseMessage = typeof shaped?.response?.data?.message === 'string'
    ? shaped.response.data.message.trim()
    : ''
  const ordinaryMessage = typeof shaped?.message === 'string' ? shaped.message.trim() : ''
  const message = responseError || responseMessage || ordinaryMessage || String(error)

  return {
    missing: message.toLowerCase().includes('database file not found'),
    message,
  }
}
