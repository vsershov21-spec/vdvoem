// Pill tab bar (navigation_tabs_v4 / navigation1) and Dynamic Island (navigation3), adapted to «Вдвоём».
import { useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Icon } from './icons'
import { haptic } from '../lib/tg'
import type { Home } from '../lib/types'
import { go } from '../lib/router'
import { relLeft } from '../lib/format'

export const TABS = [
  { path: '', id: 'home', label: 'Главная', icon: 'home' },
  { path: 'ideas', id: 'bank', label: 'Куда пойти', icon: 'folder' },
  { path: 'calendar', id: 'cal', label: 'Календарь', icon: 'cal' },
  { path: 'assistant', id: 'ai', label: 'Помощник', icon: 'spark' },
  { path: 'us', id: 'us', label: 'Мы', icon: 'user' },
]

// the donor's icon pop: squash → overshoot → settle, with a small tilt
const pop = { scale: [1, 0.72, 1.3, 0.92, 1.08, 1], rotate: [0, -14, 12, -6, 3, 0] }

function TabIcon({ icon, active }: { icon: string; active: boolean }) {
  return (
    <motion.span key={active ? 'a' : 'i'} className="pnav__ico" animate={active ? pop : { scale: 1, rotate: 0 }} transition={{ duration: 0.55, ease: [0.175, 0.885, 0.32, 1.275] }}>
      <Icon n={icon} s={21} w={active ? 2.2 : 1.9} />
    </motion.span>
  )
}

export function PillNav({ current, badge, onPlus, hidden }: { current: string; badge?: number; onPlus: () => void; hidden?: boolean }) {
  return (
    <motion.nav className="pnav" aria-label="Разделы" initial={false} animate={{ y: hidden ? 120 : 0, opacity: hidden ? 0 : 1 }} transition={{ type: 'spring', stiffness: 380, damping: 32 }}>
      <div className="pnav__bar" role="tablist">
        {TABS.map(t => {
          const active = current === t.path
          return (
            <motion.button key={t.id} id={'tab-' + t.id} role="tab" aria-selected={active} aria-label={t.label} className={'pnav__tab' + (active ? ' is-active' : '')}
              layout transition={{ layout: { type: 'spring', stiffness: 400, damping: 34 } }} whileTap={{ scale: 0.94 }}
              onClick={() => { if (!active) { haptic.select(); go(t.path) } }}>
              {active && <motion.span layoutId="pnav-pill" className="pnav__pill" transition={{ type: 'spring', stiffness: 420, damping: 34 }} />}
              <span className="pnav__layer">
                <TabIcon icon={t.icon} active={active} />
                <AnimatePresence mode="popLayout" initial={false}>
                  {active && <motion.span className="pnav__label" initial={{ opacity: 0, scale: 0.9, x: -4 }} animate={{ opacity: 1, scale: 1, x: 0 }} exit={{ opacity: 0, scale: 0.9, x: -4 }}
                    transition={{ duration: 0.18, ease: [0.16, 1, 0.3, 1] }}>{t.label}</motion.span>}
                </AnimatePresence>
              </span>
              {t.id === 'bank' && !!badge && !active && <span className="pnav__badge">{badge}</span>}
            </motion.button>
          )
        })}
      </div>
      <motion.button className="pnav__plus" aria-label="Добавить идею" whileTap={{ scale: 0.88, rotate: 90 }} onClick={() => { haptic.heavy(); onPlus() }}>
        <Icon n="plus" s={26} w={2.4} />
      </motion.button>
    </motion.nav>
  )
}

// ---------- Dynamic Island ----------

type Status = { dot: string; title: string; sub: string; go?: string; live?: boolean }

export function statusOf(h: Home | null): Status {
  if (!h) return { dot: 'var(--ink-muted)', title: 'Вдвоём', sub: '…' }
  if (!h.paired) return { dot: 'var(--warning)', title: 'Вдвоём', sub: 'ждём партнёра', go: 'us' }
  const r = h.round
  const pn = h.partner?.name || 'партнёр'
  if (r) {
    const step = ({ time: 1, vote: 2, confirm: 3 } as Record<string, number>)[r.state] || 1
    const mine = r.state === 'time' ? !r.sent : r.state === 'vote' ? !r.voted : !r.confirmed
    return { dot: mine ? 'var(--accent)' : 'var(--person-b)', title: mine ? 'Твой ход' : 'Ход: ' + pn, sub: `шаг ${step}/3 · ${relLeft(r.expires_at)}`, go: 'round/' + r.id, live: true }
  }
  if (h.next?.chosen && h.next.starts_at) {
    const d = Math.ceil((h.next.starts_at * 1000 - Date.now()) / 864e5)
    return { dot: 'var(--success)', title: h.next.chosen.title, sub: d <= 0 ? 'сегодня' : d === 1 ? 'завтра' : `через ${d} дн.`, go: 'round/' + h.next.id }
  }
  if (h.counts.unmarked) return { dot: 'var(--accent)', title: 'Вдвоём', sub: `${h.counts.unmarked} ждут отметки`, go: 'ideas' }
  return { dot: 'var(--success)', title: 'Вдвоём', sub: 'всё спокойно' }
}

export function Island({ home, actions }: { home: Home | null; actions: { id: string; label: string; icon: string; run: () => void; badge?: number }[] }) {
  const [open, setOpen] = useState(false)
  const s = statusOf(home)
  return (
    <div className="island-wrap">
      {open && <div className="island-scrim" onClick={() => setOpen(false)} />}
      <motion.div layout transition={{ type: 'spring', stiffness: 380, damping: 28, mass: 0.8 }} className={'island' + (open ? ' is-open' : '')}
        onClick={() => { if (!open) { haptic.soft(); setOpen(true) } }} role="region" aria-label="Статус и быстрые действия" data-testid="island">
        <AnimatePresence mode="wait" initial={false}>
          {!open ? (
            <motion.div key="c" className="island__compact" initial={{ opacity: 0, scale: 0.94 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0, scale: 0.94 }} transition={{ duration: 0.16 }}>
              <span className="island__mark" aria-hidden="true"><i /><i /></span>
              <b className="island__title">{s.title}</b>
              <span className={'island__dot' + (s.live ? ' is-live' : '')} style={{ ['--c' as any]: s.dot }} />
              <span className="island__sub">{s.sub}</span>
            </motion.div>
          ) : (
            <motion.div key="e" className="island__open" initial={{ opacity: 0, y: 8, scale: 0.96 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.96 }}
              transition={{ duration: 0.22, delay: 0.05 }} onClick={e => e.stopPropagation()}>
              <div className="island__head">
                <span className="island__mark island__mark--lg" aria-hidden="true"><i /><i /></span>
                <div className="island__meta"><b>{home?.me.name}{home?.partner ? ' и ' + home.partner.name : ''}</b><span>{s.title !== 'Вдвоём' ? s.title + ' · ' : ''}{s.sub}</span></div>
                <button className="island__close" aria-label="Свернуть" onClick={() => setOpen(false)}><Icon n="chev" s={16} w={2.4} style={{ transform: 'rotate(-90deg)' }} /></button>
              </div>
              <div className="island__acts">
                {actions.map(a => (
                  <motion.button key={a.id} className="island__act" whileTap={{ scale: 0.94 }} onClick={() => { haptic.tap(); setOpen(false); a.run() }}>
                    <span className="island__act-ico"><Icon n={a.icon} s={20} w={2} />{!!a.badge && <i>{a.badge}</i>}</span>
                    <span>{a.label}</span>
                  </motion.button>
                ))}
              </div>
              {s.go && <button className="island__go" onClick={() => { setOpen(false); go(s.go!) }}>Открыть <Icon n="chev" s={14} w={2.4} /></button>}
            </motion.div>
          )}
        </AnimatePresence>
      </motion.div>
    </div>
  )
}
