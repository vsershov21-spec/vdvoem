import { useEffect, useRef, useState, type ReactNode } from 'react'
import type { Idea, Person, Wish } from '../lib/types'
import { CATS } from '../lib/format'
import { haptic } from '../lib/tg'
import { Icon } from './icons'
import { back as backNav } from '../lib/router'

// ---------- Cover: photo with fade-in, or category gradient with a big glyph ----------
export function Cover({ idea, h, children, big }: { idea: Pick<Idea, 'image' | 'category' | 'title'>; h?: number; children?: ReactNode; big?: boolean }) {
  const [ok, setOk] = useState(false)
  const [broken, setBroken] = useState(false)
  const cat = CATS[idea.category] || CATS.other
  const img = idea.image && !broken
  return (
    <div className="cover" data-tone={img ? undefined : cat.tone} style={h ? { height: h } : undefined}>
      {img && <img src={idea.image} alt="" loading="lazy" referrerPolicy="no-referrer" data-loading={ok ? undefined : ''} onLoad={() => setOk(true)} onError={() => setBroken(true)} />}
      {!img && <><div className="cover__dots" /><Icon n={idea.category} s={big ? 170 : 132} w={1.2} className="cover__glyph" /></>}
      <div className="cover__in">{children}</div>
    </div>
  )
}

// ---------- Marks: my wish + partner's (hidden until I marked) ----------
export function Marks({ idea, me }: { idea: Idea; me: Person }) {
  const other = me.color === 'a' ? 'b' : 'a'
  const pw = idea.partner_wish ?? (idea.partner_marked ? 'hidden' : 'none')
  const glyph = (w: string) => w === 'very' ? <Icon n="heart" s={11} w={2.6} /> : w === 'want' ? <Icon n="check" s={11} w={3} /> : w === 'skip' ? <Icon n="x" s={10} w={3} /> : w === 'hidden' ? <span style={{ font: '800 10px/1 var(--font-sans)' }}>?</span> : null
  return (
    <span className="marks" aria-label="Отметки">
      <span className="mk" data-p={me.color} data-w={idea.my_wish || 'none'}>{glyph(idea.my_wish)}</span>
      <span className="mk" data-p={other} data-w={pw}>{glyph(pw)}</span>
    </span>
  )
}

export function WishPicker({ value, onChange, small }: { value: Wish; onChange: (w: Wish) => void; small?: boolean }) {
  const opts: [Wish, string][] = [['want', 'Хочу'], ['very', 'Очень'], ['skip', 'Пропущу']]
  return (
    <div className="wish" role="group" aria-label="Твоя оценка">
      {opts.map(([w, l]) => (
        <button key={w} className={'chip' + (small ? ' chip--sm' : '')} data-w={w} aria-pressed={value === w}
          onClick={e => { e.stopPropagation(); w === 'very' ? haptic.heavy() : haptic.select(); onChange(value === w ? '' : w) }}>
          {w === 'very' && <Icon n="heart" s={15} w={2.2} />}{l}
        </button>
      ))}
    </div>
  )
}

export function IdeaTile({ idea, me, onOpen, d = 0 }: { idea: Idea; me: Person; onOpen: () => void; d?: number }) {
  return (
    <div className="icard" style={{ ['--d' as any]: d }} onClick={() => { haptic.tap(); onOpen() }}>
      <Cover idea={idea}>
        <div className="row">{idea.liked_before && <span className="pill pill--glass"><Icon n="heart" s={12} w={2.4} />понравилось</span>}
          {!idea.parsed && <span className="pill pill--glass"><span className="spin" style={{ width: 10, height: 10, borderWidth: 2 }} />разбираю</span>}</div>
        <div className="cover__title">{idea.title}</div>
      </Cover>
      <div className="icard__meta"><span className="caption muted">{idea.meta}</span><Marks idea={idea} me={me} /></div>
    </div>
  )
}

export function Avatar({ p, ghost }: { p?: Person; ghost?: boolean }) {
  if (!p || ghost) return <span className="ava ava--ghost"><Icon n="plus" s={16} /></span>
  return <span className="ava" data-p={p.color}>{p.photo ? <img src={p.photo} alt="" /> : p.name.slice(0, 1).toUpperCase()}</span>
}

export function Segmented<T extends string>({ items, value, onChange }: { items: [T, string][]; value: T; onChange: (v: T) => void }) {
  const i = Math.max(0, items.findIndex(x => x[0] === value))
  return (
    <div className="seg" role="tablist" style={{ ['--n' as any]: items.length, ['--i' as any]: i }}>
      {items.map(([v, l]) => <button key={v} role="tab" aria-selected={v === value} onClick={() => { haptic.select(); onChange(v) }}>{l}</button>)}
    </div>
  )
}

export function Btn({ kind = 'primary', block, sm, busy, shine, children, onClick, disabled, icon }: {
  kind?: 'primary' | 'accent' | 'soft' | 'surface' | 'ghost' | 'danger'; block?: boolean; sm?: boolean; busy?: boolean; shine?: boolean
  children?: ReactNode; onClick?: () => void | Promise<void>; disabled?: boolean; icon?: string
}) {
  const [b, setB] = useState(false)
  const run = async () => {
    if (!onClick || b) return
    haptic.tap()
    const r = onClick()
    if (r instanceof Promise) { setB(true); try { await r } finally { setB(false) } }
  }
  return (
    <button className={`btn btn--${kind}${block ? ' btn--block' : ''}${sm ? ' btn--sm' : ''}${shine ? ' btn--shine' : ''}`} onClick={run} disabled={disabled}>
      {busy || b ? <span className="spin" /> : <>{icon && <Icon n={icon} s={sm ? 17 : 20} w={2} />}{children}</>}
    </button>
  )
}

// ---------- Sheet ----------
export function Sheet({ open, onClose, children }: { open: boolean; onClose: () => void; children: ReactNode }) {
  const [shown, setShown] = useState(open)
  const [out, setOut] = useState(false)
  useEffect(() => {
    if (open) { setShown(true); setOut(false) }
    else if (shown) { setOut(true); const t = setTimeout(() => setShown(false), 280); return () => clearTimeout(t) }
  }, [open])
  if (!shown) return null
  return (<>
    <div className="scrim" data-out={out || undefined} onClick={onClose} />
    <div className="sheet" data-out={out || undefined} role="dialog" aria-modal="true"><div className="sheet__grip" />{children}</div>
  </>)
}

// ---------- toast ----------
let pushToast: (t: string) => void = () => {}
export const toast = (t: string) => pushToast(t)
export function Toasts() {
  const [t, setT] = useState<{ k: number; s: string } | null>(null)
  useEffect(() => { pushToast = s => setT({ k: Date.now(), s }) }, [])
  useEffect(() => { if (!t) return; const x = setTimeout(() => setT(null), 3300); return () => clearTimeout(x) }, [t])
  return t ? <div key={t.k} className="toast" role="status">{t.s}</div> : null
}

// ---------- success burst ----------
let fire: () => void = () => {}
export const burst = () => fire()
export function Burst() {
  const [k, setK] = useState(0)
  useEffect(() => { fire = () => { haptic.ok(); setK(Date.now()) } }, [])
  useEffect(() => { if (!k) return; const t = setTimeout(() => setK(0), 1400); return () => clearTimeout(t) }, [k])
  if (!k) return null
  const cols = ['var(--person-a)', 'var(--person-b)', 'var(--accent-2)', 'var(--success)', '#ffffff']
  return (<>
    <div className="ring" key={'r' + k} />
    <div className="burst" key={k}>
      {Array.from({ length: 42 }).map((_, i) => {
        const a = (i / 42) * Math.PI * 2 + Math.random() * .4, r = 120 + Math.random() * 200
        return <i key={i} style={{ ['--c' as any]: cols[i % cols.length], ['--x' as any]: Math.cos(a) * r + 'px', ['--y' as any]: Math.sin(a) * r + 'px',
          ['--r' as any]: (Math.random() * 720 - 360) + 'deg', borderRadius: i % 3 ? 3 : '50%', width: 6 + (i % 4) * 2, height: 6 + ((i + 1) % 3) * 3,
          animationDelay: (Math.random() * 90) + 'ms' }} />
      })}
    </div>
  </>)
}

// ---------- fly-to arc: a chip flies from a point into an element (the копилка tab) ----------
export function flyTo(text: string, from: DOMRect, toSelector: string, onLand?: () => void) {
  const to = document.querySelector(toSelector)?.getBoundingClientRect()
  if (!to) { onLand?.(); return }
  const el = document.createElement('div')
  el.className = 'fly'
  el.textContent = text.length > 28 ? text.slice(0, 27) + '…' : text
  const x0 = from.left + from.width / 2, y0 = from.top + from.height / 2
  const x1 = to.left + to.width / 2, y1 = to.top + to.height / 2
  const cx = (x0 + x1) / 2 + 60, cy = Math.min(y0, y1) - 180
  el.style.offsetPath = `path("M ${x0} ${y0} Q ${cx} ${cy} ${x1} ${y1}")`
  ;(el.style as any).offsetAnchor = 'center'
  document.body.appendChild(el)
  setTimeout(() => {
    el.remove()
    const tgt = document.querySelector(toSelector) as HTMLElement | null
    if (tgt) { tgt.classList.remove('bump'); void tgt.offsetWidth; tgt.classList.add('bump') }
    haptic.soft(); onLand?.()
  }, 800)
}

// ---------- count-up ----------
export function CountUp({ to }: { to: number }) {
  const [v, setV] = useState(0)
  const prev = useRef(0)
  useEffect(() => {
    const from = prev.current; prev.current = to
    const t0 = performance.now(), d = 700
    let raf = 0
    const step = (t: number) => { const p = Math.min(1, (t - t0) / d), e = 1 - Math.pow(1 - p, 3); setV(Math.round(from + (to - from) * e)); if (p < 1) raf = requestAnimationFrame(step) }
    raf = requestAnimationFrame(step)
    return () => cancelAnimationFrame(raf)
  }, [to])
  return <>{v}</>
}

export function Skel({ h, r, mb = 10 }: { h: number; r?: number; mb?: number }) {
  return <div className="sk" style={{ height: h, borderRadius: r, marginBottom: mb }} />
}

export function Empty({ art, title, text, children }: { art?: ReactNode; title: string; text?: string; children?: ReactNode }) {
  return (
    <div className="empty rise">
      {art ?? <EmptyArt />}
      <h3 className="headline">{title}</h3>
      {text && <p className="callout muted" style={{ margin: 0, maxWidth: 290 }}>{text}</p>}
      {children}
    </div>
  )
}

// two discs drifting toward each other — the brand mark as an illustration
export function EmptyArt() {
  return (
    <svg className="empty__art" viewBox="0 0 150 110" aria-hidden="true">
      <circle cx="56" cy="55" r="36" fill="var(--person-a)" opacity=".9"><animate attributeName="cx" values="52;58;52" dur="4s" repeatCount="indefinite" /></circle>
      <circle cx="94" cy="55" r="36" fill="var(--person-b)" opacity=".9"><animate attributeName="cx" values="98;92;98" dur="4s" repeatCount="indefinite" /></circle>
      <circle cx="40" cy="20" r="3" fill="var(--line)" /><circle cx="128" cy="92" r="4" fill="var(--line)" /><circle cx="120" cy="16" r="2.5" fill="var(--hatch)" />
    </svg>
  )
}

// Sub-screen header. Inside Telegram the native BackButton is used, so ours shows only in a browser.
export function SubHeader({ title, right }: { title?: string; right?: ReactNode }) {
  const inTG = !!(window as any).Telegram?.WebApp?.initData
  return (
    <header className="hdr">
      {!inTG && <button className="iconbtn" onClick={() => backNav('')} aria-label="Назад"><Icon n="back" /></button>}
      {title && <span className="hdr__title">{title}</span>}
      <span className="spacer" />
      {right}
    </header>
  )
}
