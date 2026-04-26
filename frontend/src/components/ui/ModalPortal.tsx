import type { ReactNode } from 'react'
import ReactDOM from 'react-dom'

interface ModalPortalProps {
  children: ReactNode
}

export default function ModalPortal({ children }: ModalPortalProps) {
  if (typeof document === 'undefined') return null

  return ReactDOM.createPortal(children, document.body)
}
