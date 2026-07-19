import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import LearningApp from './LearningApp.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <LearningApp />
  </StrictMode>,
)
