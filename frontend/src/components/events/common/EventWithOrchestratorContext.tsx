import React from 'react'
import { OrchestratorContext } from './OrchestratorContext'

interface EventWithOrchestratorContextProps {
  children: React.ReactNode
  metadata?: {
    [k: string]: unknown
  }
  className?: string
}

/**
 * Generic wrapper that automatically adds orchestrator context to any event
 * that has metadata. This eliminates the need to manually add OrchestratorContext
 * to every individual event component.
 */
export const EventWithOrchestratorContext: React.FC<EventWithOrchestratorContextProps> = ({ 
  children, 
  metadata, 
  className = "" 
}) => {
  
  return (
    <div className={className}>
      {/* Automatically add orchestrator context if metadata exists */}
      <OrchestratorContext metadata={metadata} className="mb-2" />
      {children}
    </div>
  )
}

