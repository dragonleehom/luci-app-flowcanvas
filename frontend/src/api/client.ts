import { demoSnapshot } from './demo'
import type { APIEnvelope, CanvasSnapshot, CompilationResult, GraphSaveRequest } from '../types/api'

const isLuCI = window.location.pathname.includes('/luci-static/resources/flowcanvas')
const apiBase = import.meta.env.VITE_FLOWCANVAS_API_BASE ?? (isLuCI ? '/cgi-bin/luci/admin/services/flowcanvas/api/v1' : '/api/v1')

export class FlowCanvasAPIError extends Error {
  public readonly code: string
  public readonly details?: Record<string, unknown>

  constructor(code: string, message: string, details?: Record<string, unknown>) {
    super(message)
    this.name = 'FlowCanvasAPIError'
    this.code = code
    this.details = details
  }
}

export async function loadCanvas(signal?: AbortSignal): Promise<CanvasSnapshot> {
  try {
    const response = await fetch(`${apiBase}/canvas`, {
      headers: { Accept: 'application/json' },
      signal,
    })
    return await unwrap<CanvasSnapshot>(response)
  } catch (error) {
    if (import.meta.env.DEV && isNetworkError(error)) {
      return structuredClone(demoSnapshot)
    }
    throw error
  }
}

export async function saveGraph(
  etag: string,
  graph: GraphSaveRequest,
  signal?: AbortSignal,
): Promise<CanvasSnapshot> {
  const response = await fetch(appendToken(`${apiBase}/canvas/graph`), {
    method: isLuCI ? 'POST' : 'PUT',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      'If-Match': `"${etag}"`,
    },
    body: JSON.stringify(graph),
    signal,
  })
  return unwrap<CanvasSnapshot>(response)
}

export async function refreshDiscovery(signal?: AbortSignal): Promise<void> {
  try {
    const response = await fetch(appendToken(`${apiBase}/discovery/refresh`), {
      method: 'POST',
      headers: { Accept: 'application/json' },
      signal,
    })
    await unwrap<Record<string, unknown>>(response)
  } catch (error) {
    if (import.meta.env.DEV && isNetworkError(error)) {
      return
    }
    throw error
  }
}

function getLuCIToken(): string | undefined {
  if (isLuCI && window.parent && (window.parent as any).L && (window.parent as any).L.env) {
    return (window.parent as any).L.env.token
  }
  return undefined
}

function appendToken(url: string): string {
  const token = getLuCIToken()
  if (!token) return url
  const separator = url.includes('?') ? '&' : '?'
  return `${url}${separator}token=${encodeURIComponent(token)}`
}

export async function validateCompilation(signal?: AbortSignal): Promise<CompilationResult> {
  const response = await fetch(appendToken(`${apiBase}/compilations/validate`), {
    method: 'POST',
    headers: { Accept: 'application/json' },
    signal,
  })
  return unwrap<CompilationResult>(response)
}

export async function applyCompilation(etag: string, signal?: AbortSignal): Promise<CompilationResult> {
  const response = await fetch(appendToken(`${apiBase}/compilations/apply`), {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'If-Match': `"${etag}"`,
    },
    signal,
  })
  return unwrap<CompilationResult>(response)
}

export function subscribeCanvasEvents(onResync: () => void): () => void {
  if (import.meta.env.DEV || isLuCI) {
    // LuCI CGI proxying does not support long-lived SSE connections.
    // In LuCI mode, we fallback to a simple polling mechanism.
    const interval = setInterval(onResync, 10000)
    return () => clearInterval(interval)
  }
  const events = new EventSource(`${apiBase}/canvas/events`)
  const handler = () => onResync()
  events.addEventListener('canvas.patch', handler)
  events.addEventListener('resync', handler)
  events.onerror = () => {
    // EventSource reconnects itself; a refetch is safer once the stream resumes.
  }
  return () => events.close()
}

async function unwrap<T>(response: Response): Promise<T> {
  const payload = (await response.json()) as APIEnvelope<T> | { error?: { code: string; message: string; details?: Record<string, unknown> } }
  if (!response.ok || !('data' in payload)) {
    const error = 'error' in payload ? payload.error : undefined
    throw new FlowCanvasAPIError(
      error?.code ?? `HTTP_${response.status}`,
      error?.message ?? `请求失败（HTTP ${response.status}）。`,
      error?.details,
    )
  }
  return payload.data
}

function isNetworkError(error: unknown): boolean {
  return error instanceof TypeError || (error instanceof DOMException && error.name !== 'AbortError')
}
