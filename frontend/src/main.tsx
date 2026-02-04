import './wdyr';
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import ServerConnectionStatus from './components/ServerConnectionStatus'

createRoot(document.getElementById('root')!).render(
  <ServerConnectionStatus>
    <App />
  </ServerConnectionStatus>
)
