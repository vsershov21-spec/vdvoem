import { createRoot } from 'react-dom/client'
import '@fontsource/onest/cyrillic-400.css'
import '@fontsource/onest/cyrillic-500.css'
import '@fontsource/onest/cyrillic-600.css'
import '@fontsource/onest/cyrillic-700.css'
import '@fontsource/onest/cyrillic-800.css'
import '@fontsource/onest/latin-400.css'
import '@fontsource/onest/latin-600.css'
import '@fontsource/onest/latin-800.css'
import './ui/tokens.css'
import './ui/app.css'
import { App } from './App'
import { ready } from './lib/tg'

clearTimeout((window as any).__vdBoot)
try { sessionStorage.removeItem('vd-reload') } catch { /* private mode */ }
ready()
createRoot(document.getElementById('root')!).render(<App />)
