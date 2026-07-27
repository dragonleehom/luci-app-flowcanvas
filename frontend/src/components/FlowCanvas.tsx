import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  addEdge,
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  ReactFlow,
  useEdgesState,
  useNodesState,
  type Connection,
  type Edge,
  type IsValidConnection,
  type Node,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'

import { applyCompilation, FlowCanvasAPIError, loadCanvas, refreshDiscovery, saveGraph, subscribeCanvasEvents, validateCompilation } from '../api/client'
import type {
  CanvasEdge,
  CanvasNode,
  CanvasSnapshot,
  CompilationResult,
  FilterNodeData,
  NodeData,
  NodeKind,
  SourceNodeData,
} from '../types/api'
import { FilterNode, SourceNode, TargetNode } from './nodes'

type FlowNode = Node<NodeData, NodeKind>
type FlowEdge = Edge<{ kind?: CanvasEdge['kind'] }>

const nodeTypes = {
  source: SourceNode,
  filter: FilterNode,
  target: TargetNode,
}

export function FlowCanvas() {
  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<FlowEdge>([])
  const [snapshot, setSnapshot] = useState<CanvasSnapshot | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [compiling, setCompiling] = useState(false)
  const [compilation, setCompilation] = useState<CompilationResult | null>(null)
  const [message, setMessage] = useState<string>('正在建立本地网络意图视图。')

  const applySnapshot = useCallback((next: CanvasSnapshot) => {
    setSnapshot(next)
    setNodes(next.nodes.map(toFlowNode))
    setEdges(next.edges.map(toFlowEdge))
  }, [setEdges, setNodes])

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const next = await loadCanvas()
      applySnapshot(next)
      setMessage(`已同步第 ${next.canvas.revision} 版画布。`)
    } catch (error) {
      setMessage(readError(error))
    } finally {
      setLoading(false)
    }
  }, [applySnapshot])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => subscribeCanvasEvents(() => void refresh()), [refresh])

  const nodeByID = useMemo(() => new Map(nodes.map((node) => [node.id, node])), [nodes])

  const isValidConnection = useCallback<IsValidConnection>((connection) => {
    if (!connection.source || !connection.target) {
      return false
    }
    const source = nodeByID.get(connection.source)
    const target = nodeByID.get(connection.target)
    if (!source || !target || source.id === target.id) {
      return false
    }
    if (source.type === 'source' && target.type === 'filter') {
      const sourceData = source.data as SourceNodeData
      const filterData = target.data as FilterNodeData
      return sourceData.deviceId === filterData.deviceId
    }
    return source.type === 'filter' && target.type === 'target'
  }, [nodeByID])

  const onConnect = useCallback((connection: Connection) => {
    if (!isValidConnection(connection) || !connection.source || !connection.target) {
      setMessage('已拒绝：只允许同一终端的 Source → Filter，以及 Filter → Target 连线。')
      return
    }
    const duplicate = edges.some((edge) => edge.source === connection.source && edge.target === connection.target)
    if (duplicate) {
      setMessage('该连线已经存在。')
      return
    }
    const kind = nodeByID.get(connection.source)?.type === 'source' ? 'source_to_filter' : 'filter_to_target'
    setEdges((current) => addEdge({
      ...connection,
      id: `edge-${connection.source}-${connection.target}`,
      type: 'smoothstep',
      animated: true,
      data: { kind },
      className: `flow-edge flow-edge--${kind}`,
    }, current))
    setMessage('连线已加入草稿；点击“保存编排”后写入控制面。')
  }, [edges, isValidConnection, nodeByID, setEdges])

  const requestDiscoveryRefresh = useCallback(async () => {
    setLoading(true)
    try {
      await refreshDiscovery()
      const next = await loadCanvas()
      applySnapshot(next)
      setMessage('已完成 Mihomo、ARP/DHCP 与出口目录刷新。')
    } catch (error) {
      setMessage(readError(error))
    } finally {
      setLoading(false)
    }
  }, [applySnapshot])

  const previewRules = useCallback(async () => {
    if (!snapshot) {
      return
    }
    setCompiling(true)
    try {
      const result = await validateCompilation()
      setCompilation(result)
      setMessage(`规则预览已生成：${result.preview.providers.length} 个出口分组、${result.preview.rules.length} 条顶层规则。`)
    } catch (error) {
      setMessage(readError(error))
    } finally {
      setCompiling(false)
    }
  }, [snapshot])

  const applyRules = useCallback(async () => {
    if (!snapshot || !compilation) {
      setMessage('请先保存编排并生成规则预览。')
      return
    }
    const accepted = window.confirm(`将当前第 ${snapshot.canvas.revision} 版画布规则写入 Mihomo 并热重载。若内核拒绝候选配置，系统会自动回滚到备份版本。是否继续？`)
    if (!accepted) {
      setMessage('已取消应用规则。')
      return
    }
    setCompiling(true)
    try {
      const result = await applyCompilation(snapshot.canvas.etag)
      setCompilation(result)
      setMessage(`Mihomo 已应用规则，审计版本 ${result.compilation.id.slice(0, 16)}。`)
      await refresh()
    } catch (error) {
      setMessage(readError(error))
      if (error instanceof FlowCanvasAPIError && error.code === 'CANVAS_REVISION_CONFLICT') {
        await refresh()
      }
    } finally {
      setCompiling(false)
    }
  }, [compilation, refresh, snapshot])

  const persist = useCallback(async () => {
    if (!snapshot) {
      return
    }
    setSaving(true)
    try {
      const graph = {
        nodePositions: nodes.map(({ id, position }) => ({ id, position })),
        edges: edges.map(toCanvasEdge),
      }
      const next = await saveGraph(snapshot.canvas.etag, graph)
      applySnapshot(next)
      setMessage(`已保存第 ${next.canvas.revision} 版编排草图。`)
    } catch (error) {
      setMessage(readError(error))
      if (error instanceof FlowCanvasAPIError && error.code === 'CANVAS_REVISION_CONFLICT') {
        await refresh()
      }
    } finally {
      setSaving(false)
    }
  }, [applySnapshot, edges, nodes, refresh, snapshot])

  return (
    <main className="console-shell">
      <header className="console-header">
        <div>
          <p className="console-header__eyebrow">Luci App · Intent-Based Networking</p>
          <h1>FlowCanvas 控制台</h1>
          <p className="console-header__subline">仅展示 Mihomo 真实观察到的终端、动态域名特征与可用出口。</p>
        </div>
        <div className="console-header__actions">
          <button className="button button--ghost" type="button" onClick={() => void requestDiscoveryRefresh()} disabled={loading || saving || compiling}>
            {loading ? '同步中…' : '刷新发现'}
          </button>
          <button className="button button--ghost" type="button" onClick={() => void previewRules()} disabled={!snapshot || loading || saving || compiling}>
            {compiling ? '处理中…' : '预览规则'}
          </button>
          <button className="button button--danger" type="button" onClick={() => void applyRules()} disabled={!snapshot || !compilation || loading || saving || compiling}>
            应用至 Mihomo
          </button>
          <button className="button button--primary" type="button" onClick={() => void persist()} disabled={!snapshot || loading || saving || compiling}>
            {saving ? '保存中…' : '保存编排'}
          </button>
        </div>
      </header>

      <section className="console-status" aria-live="polite">
        <span className={`status-pulse ${loading ? 'status-pulse--loading' : ''}`} />
        <span>{message}</span>
        {snapshot ? <span className="status-revision">版本 {snapshot.canvas.revision}</span> : null}
      </section>

      <section className="workspace">
        <aside className="workspace__legend" aria-label="画布说明">
          <div className="legend-section">
            <h2>意图链路</h2>
            <p>仅支持 <strong>终端 → 动态应用流 → Mihomo 出口</strong>。Filter 只能接收其所属终端的连线。</p>
          </div>
          <div className="legend-section">
            <h2>状态语义</h2>
            <dl className="legend-list">
              <div><dt><span className="state-dot state-dot--active" /></dt><dd>活跃：当前连接或可用出口。</dd></div>
              <div><dt><span className="state-dot state-dot--inactive" /></dt><dd>历史：已观察但当前没有活动连接。</dd></div>
            </dl>
          </div>
          <div className="legend-section legend-section--notice">
            <h2>实时发现</h2>
            <p>画布会订阅连接状态变更；“刷新发现”会即时读取 Mihomo 出口、ARP 表与 DHCP 租约。保存后先预览受管规则，再显式确认应用至 Mihomo。</p>
          </div>
          {compilation ? <div className="legend-section compilation-panel">
            <h2>规则审计</h2>
            <p><strong>{compilation.compilation.status}</strong> · {compilation.preview.providers.length} 个出口分组 · {compilation.preview.rules.length} 条规则</p>
            {compilation.preview.warnings.map((warning) => <p className="compilation-panel__warning" key={warning}>{warning}</p>)}
            {compilation.rollback ? <p>回滚状态：<strong>{compilation.rollback.status}</strong></p> : null}
            <details>
              <summary>查看受管 YAML</summary>
              <pre>{compilation.preview.managedYaml}</pre>
            </details>
          </div> : null}
        </aside>

        <div className="workspace__canvas" aria-label="网络意图编排画布">
          <ReactFlow<FlowNode, FlowEdge>
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            isValidConnection={isValidConnection}
            fitView
            minZoom={0.2}
            maxZoom={2.5}
            defaultEdgeOptions={{ type: 'smoothstep', animated: true }}
            proOptions={{ hideAttribution: true }}
          >
            <Background variant={BackgroundVariant.Dots} gap={22} size={1.2} color="rgba(139, 184, 255, 0.22)" />
            <Controls showInteractive={false} />
            <MiniMap zoomable pannable nodeColor={(node) => node.type === 'source' ? '#4ecdc4' : node.type === 'filter' ? '#9e8cff' : '#ffb86b'} />
          </ReactFlow>
        </div>
      </section>
    </main>
  )
}

function toFlowNode(node: CanvasNode): FlowNode {
  return {
    id: node.id,
    type: node.kind,
    position: node.position,
    data: node.data,
  } as FlowNode
}

function toFlowEdge(edge: CanvasEdge): FlowEdge {
  return {
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: 'smoothstep',
    animated: true,
    className: edge.kind ? `flow-edge flow-edge--${edge.kind}` : undefined,
    data: { kind: edge.kind },
  }
}

function toCanvasEdge(edge: FlowEdge): CanvasEdge {
  return {
    id: edge.id,
    source: edge.source,
    target: edge.target,
    kind: edge.data?.kind,
  }
}

function readError(error: unknown): string {
  if (error instanceof FlowCanvasAPIError) {
    return `${error.code}：${error.message}`
  }
  if (error instanceof Error) {
    return error.message
  }
  return '发生未知错误。'
}
