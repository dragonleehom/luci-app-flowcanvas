export type NodeKind = 'source' | 'filter' | 'target'
export type ResourceState = 'active' | 'inactive' | 'unknown'
export type Network = 'tcp' | 'udp' | 'quic' | 'unknown'
export type MatchKind = 'domain' | 'suffix' | 'keyword'

export interface Position {
  x: number
  y: number
}

export interface MatchSpec {
  kind: MatchKind
  value: string
}

export interface SourceNodeData {
  [key: string]: unknown
  deviceId: string
  label: string
  ip: string
  mac?: string
  state: ResourceState
  lastSeenAt: string
}

export interface FilterNodeData {
  [key: string]: unknown
  deviceApplicationId: string
  deviceId: string
  observedHost: string
  network: Network
  transportHint?: string
  state: ResourceState
  activeConnections: number
  match: MatchSpec
  firstSeenAt: string
  lastSeenAt: string
}

export interface TargetNodeData {
  [key: string]: unknown
  proxyName: string
  proxyType: string
  alive: boolean
  udp: boolean
  state: ResourceState
}

export type NodeData = SourceNodeData | FilterNodeData | TargetNodeData

export interface CanvasNode {
  id: string
  kind: NodeKind
  position: Position
  data: NodeData
}

export type EdgeKind = 'source_to_filter' | 'filter_to_target'

export interface CanvasEdge {
  id: string
  source: string
  target: string
  kind?: EdgeKind
}

export interface CanvasSnapshot {
  canvas: {
    id: string
    name: string
    revision: number
    etag: string
    updatedAt: string
  }
  nodes: CanvasNode[]
  edges: CanvasEdge[]
  discovery: {
    connectionsUpdatedAt?: string
    devicesUpdatedAt?: string
    targetsUpdatedAt?: string
  }
}

export interface GraphSaveRequest {
  nodePositions: Array<{ id: string; position: Position }>
  edges: CanvasEdge[]
}

export interface APIEnvelope<T> {
  requestId?: string
  data: T
}

export interface APIError {
  requestId?: string
  error: {
    code: string
    message: string
    details?: Record<string, unknown>
  }
}
