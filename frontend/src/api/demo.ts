import type { CanvasSnapshot } from '../types/api'

const timestamp = '2026-07-27T12:00:00Z'

export const demoSnapshot: CanvasSnapshot = {
  canvas: {
    id: 'default',
    name: '默认编排图',
    revision: 0,
    etag: 'canvas-0',
    updatedAt: timestamp,
  },
  nodes: [
    {
      id: 'source:dev-demo-tv',
      kind: 'source',
      position: { x: 80, y: 190 },
      data: {
        deviceId: 'dev-demo-tv',
        label: 'LivingRoom-TV',
        ip: '192.168.1.50',
        mac: '00:11:22:33:44:55',
        state: 'active',
        lastSeenAt: timestamp,
      },
    },
    {
      id: 'filter:da-demo-qq',
      kind: 'filter',
      position: { x: 460, y: 190 },
      data: {
        deviceApplicationId: 'da-demo-qq',
        deviceId: 'dev-demo-tv',
        observedHost: 'v.qq.com',
        network: 'tcp',
        transportHint: 'tls',
        state: 'active',
        activeConnections: 2,
        match: { kind: 'domain', value: 'v.qq.com' },
        firstSeenAt: '2026-07-27T10:00:00Z',
        lastSeenAt: timestamp,
      },
    },
    {
      id: 'filter:da-demo-video',
      kind: 'filter',
      position: { x: 460, y: 420 },
      data: {
        deviceApplicationId: 'da-demo-video',
        deviceId: 'dev-demo-tv',
        observedHost: 'googlevideo.com',
        network: 'quic',
        transportHint: 'quic',
        state: 'inactive',
        activeConnections: 0,
        match: { kind: 'domain', value: 'googlevideo.com' },
        firstSeenAt: '2026-07-26T12:00:00Z',
        lastSeenAt: '2026-07-27T11:36:00Z',
      },
    },
    {
      id: 'target:tailscale0',
      kind: 'target',
      position: { x: 840, y: 190 },
      data: {
        proxyName: 'tailscale0',
        proxyType: 'DIRECT',
        alive: true,
        udp: true,
        state: 'active',
      },
    },
    {
      id: 'target:proxy-group-us',
      kind: 'target',
      position: { x: 840, y: 430 },
      data: {
        proxyName: 'Proxy-US',
        proxyType: 'URLTest',
        alive: false,
        udp: true,
        state: 'inactive',
      },
    },
  ],
  edges: [
    {
      id: 'edge-demo-source-filter',
      source: 'source:dev-demo-tv',
      target: 'filter:da-demo-qq',
      kind: 'source_to_filter',
    },
    {
      id: 'edge-demo-filter-target',
      source: 'filter:da-demo-qq',
      target: 'target:tailscale0',
      kind: 'filter_to_target',
    },
  ],
  discovery: {
    connectionsUpdatedAt: timestamp,
    devicesUpdatedAt: timestamp,
    targetsUpdatedAt: timestamp,
  },
}
