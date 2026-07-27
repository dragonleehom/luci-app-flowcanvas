import { demoSnapshot } from './demo'
import type { APIEnvelope, CanvasSnapshot, GraphSaveRequest } from '../types/api'

const apiBase = import.meta.env.VITE_FLOWCANVAS_API_BASE ?? '/api/v1'

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
  const response = await fetch(`${apiBase}/canvas/graph`, {
    method: 'PUT',
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
    const response = await fetch(`${apiBase}/discovery/refresh`, {
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

export function subscribeCanvasEvents(onResync: () => void): () => void {
  if (import.meta.env.DEV) {
    return () => undefined
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
