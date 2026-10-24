import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'
import { upgradeLegacyHash } from './router'

upgradeLegacyHash()

try {
  if (localStorage.getItem('haven-theme') === 'bone') document.documentElement.dataset.theme = 'bone'
} catch {
  /* storage may be blocked */
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
