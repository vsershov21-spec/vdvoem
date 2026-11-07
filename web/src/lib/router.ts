import { useEffect, useState } from 'react'
// Paths live under /app/: "", "ideas", "idea/12", "round/5", "assistant", "us", "settings", "history"
const BASE = '/app/'
const cur = () => location.pathname.startsWith(BASE) ? location.pathname.slice(BASE.length).replace(/\/$/, '') : ''
const subs = new Set<() => void>()
let dir: 'fwd' | 'back' = 'fwd'
export const navDir = () => dir
export function go(path: string, replace = false) {
  dir = 'fwd'
  const url = BASE + path.replace(/^\//, '')
  if (replace) history.replaceState({ d: depth() }, '', url)
  else history.pushState({ d: depth() + 1 }, '', url)
  subs.forEach(f => f())
}
const depth = () => (history.state?.d as number) || 0
export const canBack = () => depth() > 0
export function back(fallback = '') {
  if (canBack()) { dir = 'back'; history.back() } else go(fallback, true)
}
window.addEventListener('popstate', () => { dir = 'back'; subs.forEach(f => f()) })
export function useRoute(): string[] {
  const [p, setP] = useState(cur())
  useEffect(() => { const f = () => setP(cur()); subs.add(f); return () => { subs.delete(f) } }, [])
  return p ? p.split('/') : ['']
}
