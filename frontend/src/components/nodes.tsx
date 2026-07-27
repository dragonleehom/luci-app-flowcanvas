import { Handle, Position, type Node, type NodeProps } from '@xyflow/react'
import type { FilterNodeData, SourceNodeData, TargetNodeData } from '../types/api'

type SourceFlowNode = Node<SourceNodeData, 'source'>
type FilterFlowNode = Node<FilterNodeData, 'filter'>
type TargetFlowNode = Node<TargetNodeData, 'target'>

function StateDot({ state }: { state: 'active' | 'inactive' | 'unknown' }) {
  return <span className={`state-dot state-dot--${state}`} aria-label={state} />
}

export function SourceNode({ data, selected }: NodeProps<SourceFlowNode>) {
  return (
    <article className={`flow-node flow-node--source flow-node--${data.state} ${selected ? 'is-selected' : ''}`}>
      <header className="flow-node__header">
        <span className="flow-node__eyebrow">终端设备</span>
        <StateDot state={data.state} />
      </header>
      <strong className="flow-node__title">{data.label}</strong>
      <p className="flow-node__primary">{data.ip}</p>
      <p className="flow-node__secondary">{data.mac ?? 'MAC 未解析'}</p>
      <Handle type="source" position={Position.Right} id="source-out" className="flow-handle flow-handle--out" />
    </article>
  )
}

export function FilterNode({ data, selected }: NodeProps<FilterFlowNode>) {
  return (
    <article className={`flow-node flow-node--filter flow-node--${data.state} ${selected ? 'is-selected' : ''}`}>
      <Handle type="target" position={Position.Left} id="filter-in" className="flow-handle flow-handle--in" />
      <header className="flow-node__header">
        <span className="flow-node__eyebrow">动态应用流</span>
        <span className={`protocol-chip protocol-chip--${data.network}`}>{data.network.toUpperCase()}</span>
      </header>
      <strong className="flow-node__title">{data.observedHost}</strong>
      <p className="flow-node__primary">{data.match.kind.toUpperCase()} · {data.match.value}</p>
      <p className="flow-node__secondary">
        {data.state === 'active' ? `${data.activeConnections} 条活跃连接` : '历史嗅探记录'}
      </p>
      <Handle type="source" position={Position.Right} id="filter-out" className="flow-handle flow-handle--out" />
    </article>
  )
}

export function TargetNode({ data, selected }: NodeProps<TargetFlowNode>) {
  return (
    <article className={`flow-node flow-node--target flow-node--${data.state} ${selected ? 'is-selected' : ''}`}>
      <Handle type="target" position={Position.Left} id="target-in" className="flow-handle flow-handle--in" />
      <header className="flow-node__header">
        <span className="flow-node__eyebrow">Mihomo 出口</span>
        <StateDot state={data.state} />
      </header>
      <strong className="flow-node__title">{data.proxyName}</strong>
      <p className="flow-node__primary">{data.proxyType}</p>
      <p className="flow-node__secondary">{data.udp ? 'UDP 可用' : '仅 TCP'} · {data.alive ? '在线' : '离线'}</p>
    </article>
  )
}
