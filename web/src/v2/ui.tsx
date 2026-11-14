import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { go } from '../lib/router'
import { haptic } from '../lib/tg'
import { sceneFor } from './art'

// ---------- bottom panel: glass capsule, the active tab's pad slides on a spring ----------
export const TABS = [
  { path: '', name: 'Главная', icon: '<path d="M4 11l8-7 8 7v8a1 1 0 0 1-1 1h-4v-6h-6v6H5a1 1 0 0 1-1-1z"/>' },
  { path: 'ideas', name: 'Идеи', icon: '<path d="M12 20s-7-4.4-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 10c0 5.600-7 10-7 10z"/>' },
  { path: 'assistant', name: 'Помощник', icon: '<path d="M12 3l1.900 5.100L19 10l-5.100 1.900L12 17l-1.900-5.100L5 10l5.100-1.900z"/><path d="M18.500 16l.700 1.800 1.800.700-1.800.700-.700 1.800-.700-1.800-1.800-.700 1.800-.700z"/>' },
  { path: 'chores', name: 'Дела', icon: '<rect x="4.500" y="4.500" width="15" height="15" rx="4"/><path d="M8.500 12.200l2.400 2.400 4.600-5"/>' },
  { path: 'plans', name: 'Планы', icon: '<rect x="4.500" y="5.500" width="15" height="14" rx="3.500"/><path d="M4.500 10h15M9 3.500v4M15 3.500v4"/>' },
]
let navPrev = 0
export function Nav({ tab, counts }: { tab: string; counts: Record<string, number> }) {
  const ix = TABS.findIndex(t => t.path === tab)
  const from = navPrev
  const just = useRef(tab)
  const fresh = just.current !== tab
  useEffect(() => { if (ix >= 0 && ix !== 2) navPrev = ix; just.current = tab }, [ix, tab])
  return (
    <nav className="nav" aria-label="Разделы">
      {ix >= 0 && ix !== 2 && <span className="v-pill" key={ix} style={{ '--from': from * 100 + '%', '--to': ix * 100 + '%' } as CSSProperties} />}
      {TABS.map(t => {
        const on = t.path === tab, c = counts[t.path] || 0
        return (
          <button key={t.path} className={(t.path === 'assistant' ? 'ai ' : '') + (on ? 'on' : '') + (on && fresh ? ' just' : '')} aria-current={on ? 'page' : undefined}
            data-testid={'tab-' + (t.path || 'home')} onClick={() => { haptic.tap?.(); go(t.path) }}>
            <span className="g"><svg viewBox="0 0 24 24" dangerouslySetInnerHTML={{ __html: t.icon }} /></span>{t.name}
            {c > 0 && !on && <span className="cnt">{c}</span>}
          </button>)
      })}
    </nav>)
}

// ---------- the confirmation line: every action answers, reversible ones offer «Вернуть» ----------
type ToastT = { k: number; text: string; undo?: () => void }
let push: (t: Omit<ToastT, 'k'>) => void = () => {}
export const say = (text: string, undo?: () => void) => push({ text, undo })
export function Toast() {
  const [t, setT] = useState<ToastT | null>(null)
  useEffect(() => { push = x => setT({ ...x, k: Date.now() }) }, [])
  useEffect(() => { if (!t) return; const x = setTimeout(() => setT(null), t.undo ? 5000 : 3000); return () => clearTimeout(x) }, [t])
  if (!t) return null
  return (
    <div className="v-toast" key={t.k} role="status" data-testid="toast">
      <svg className="tick" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9.5" pathLength={1} /><path d="M7.5 12.3l3 3 6-6.6" pathLength={1} /></svg>
      <span>{t.text}</span>
      {t.undo && <button onClick={() => { t.undo!(); setT(null) }}>Вернуть</button>}
    </div>)
}

// ---------- sheet: glass, from the bottom; a tap on the dimmed area closes it ----------
export function Sheet({ open, onClose, children, testid }: { open: boolean; onClose: () => void; children: ReactNode; testid?: string }) {
  useEffect(() => {
    if (!open) return
    const f = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', f)
    return () => window.removeEventListener('keydown', f)
  }, [open, onClose])
  if (!open) return null
  return (
    <div className="v-scrim" onClick={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="v-sheet" role="dialog" aria-modal="true" data-testid={testid}>{children}</div>
    </div>)
}

// ---------- a picture: the idea's photo, or a drawn scene over a tone until there is one ----------
export function Pic({ idea, className = 'pic', style, children, ...rest }: { idea: { title: string; category?: string; image?: string }; className?: string; style?: CSSProperties; children?: ReactNode } & Record<string, unknown>) {
  if (idea.image) return <span className={className + ' photo'} style={{ backgroundImage: `url(${idea.image})`, ...style }} {...rest}>{children}</span>
  const s = sceneFor(idea)
  return <span className={className + ' ' + s.tone} style={style} {...rest}><span style={{ display: 'contents' }} dangerouslySetInnerHTML={{ __html: s.svg }} />{children}</span>
}

export const Empty = ({ title, children }: { title: string; children?: ReactNode }) => (
  <div className="v-card stub">
    <svg className="eart" viewBox="0 0 120 64" aria-hidden="true"><circle cx="46" cy="32" r="22" fill="none" stroke="currentColor" strokeWidth="2" strokeDasharray="2 6" strokeLinecap="round" /><circle cx="74" cy="32" r="22" fill="none" stroke="var(--accent)" strokeWidth="2" strokeDasharray="2 6" strokeLinecap="round" /><circle cx="60" cy="32" r="3.5" fill="var(--accent)" /></svg>
    <h3>{title}</h3>{children}
  </div>)
