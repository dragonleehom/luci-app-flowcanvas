import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { FlowCanvas } from './components/FlowCanvas'
import './styles.css'

const root = document.getElementById('root')
if (!root) {
  throw new Error('找不到 FlowCanvas 根节点。')
}

createRoot(root).render(
  <StrictMode>
    <FlowCanvas />
  </StrictMode>,
)
