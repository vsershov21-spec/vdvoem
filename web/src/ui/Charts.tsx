// Charts and month calendar in the dashboard_v7 «Glass Fitness» style (orange theme): metric cards with mini graphs,
// smooth area chart with a peak bubble, half-gauge, and a month grid with coloured days.
import { useId, useMemo } from 'react'
import { motion } from 'framer-motion'
import { Icon } from './icons'

export const TONE: Record<string, { c: string; soft: string; mid: string }> = {
  orange: { c: '#ff701e', soft: '#ffd1b3', mid: '#ffa870' },
  pink: { c: '#ff3b71', soft: '#ffc2d3', mid: '#ff8aa8' },
  green: { c: '#22c55e', soft: '#bbf0cf', mid: '#6ad992' },
  blue: { c: '#3b82f6', soft: '#93c5fd', mid: '#60a5fa' },
}

export function Bars({ values, tone = 'orange', h = 28 }: { values: number[]; tone?: string; h?: number }) {
  const t = TONE[tone], max = Math.max(1, ...values), n = values.length, step = 100 / n
  return (
    <svg width="100%" height={h} viewBox={`0 0 100 ${h}`} preserveAspectRatio="none" aria-hidden="true">
      {values.map((v, i) => {
        const bh = Math.max(3, (v / max) * (h - 2)), fill = v === max ? t.c : v > max / 2 ? t.mid : t.soft
        return <motion.rect key={i} x={i * step + step / 2 - 1.6} width="3.2" rx="1.6" fill={fill} initial={{ height: 0, y: h }} animate={{ height: bh, y: h - bh }} transition={{ delay: i * 0.05, type: 'spring', stiffness: 200, damping: 20 }} />
      })}
    </svg>
  )
}

export function Pulse({ tone = 'pink' }: { tone?: string }) {
  return (
    <svg width="100%" height="28" viewBox="0 0 100 28" fill="none" aria-hidden="true">
      <motion.path d="M2 14h18l4-10 6 22 5-16 4 9 4-5h12l3-7 5 15 4-11 3 7h24" stroke={TONE[tone].c} strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"
        initial={{ pathLength: 0 }} animate={{ pathLength: 1 }} transition={{ duration: 1.1, ease: 'easeInOut' }} />
    </svg>
  )
}

export function Gauge({ value, tone = 'green', size = 56 }: { value: number; tone?: string; size?: number }) {
  const L = Math.PI * 22, p = Math.max(0, Math.min(100, value)) / 100
  return (
    <svg width={size} height={size * 0.6} viewBox="0 0 56 34" aria-hidden="true">
      <path d="M 6 30 A 22 22 0 0 1 50 30" fill="none" stroke="var(--line)" strokeWidth="5.5" strokeLinecap="round" />
      <motion.path d="M 6 30 A 22 22 0 0 1 50 30" fill="none" stroke={TONE[tone].c} strokeWidth="5.5" strokeLinecap="round" strokeDasharray={L}
        initial={{ strokeDashoffset: L }} animate={{ strokeDashoffset: L * (1 - p) }} transition={{ duration: 1, ease: [0.34, 1.56, 0.64, 1] }} />
    </svg>
  )
}

export function Metric({ label, icon, tone, value, unit, graph, onClick }: { label: string; icon: string; tone: string; value: React.ReactNode; unit: string; graph: React.ReactNode; onClick?: () => void }) {
  return (
    <button className="metric" onClick={onClick} style={{ ['--tc' as any]: TONE[tone].c }}>
      <span className="metric__top"><span className="metric__label">{label}</span><span className="metric__badge"><Icon n={icon} s={13} w={2.2} /></span></span>
      <span className="metric__graph">{graph}</span>
      <span className="metric__bottom"><b>{value}</b><span>{unit}</span></span>
    </button>
  )
}

// Catmull-Rom → cubic Bézier, so the line passes through every point and stays smooth
function smooth(pts: [number, number][]) {
  if (pts.length < 2) return ''
  let d = `M ${pts[0][0]} ${pts[0][1]}`
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i - 1] || pts[i], p1 = pts[i], p2 = pts[i + 1], p3 = pts[i + 2] || p2
    const c1 = [p1[0] + (p2[0] - p0[0]) / 6, p1[1] + (p2[1] - p0[1]) / 6], c2 = [p2[0] - (p3[0] - p1[0]) / 6, p2[1] - (p3[1] - p1[1]) / 6]
    d += ` C ${c1[0].toFixed(1)} ${c1[1].toFixed(1)}, ${c2[0].toFixed(1)} ${c2[1].toFixed(1)}, ${p2[0]} ${p2[1]}`
  }
  return d
}

export function AreaChart({ labels, values, unit = '', peakLabel }: { labels: string[]; values: number[]; unit?: string; peakLabel?: (v: number) => string }) {
  const id = useId().replace(/:/g, '')
  const W = 340, H = 140, top = 26, bottom = 112, left = 26, right = 330
  const max = Math.max(1, ...values)
  const pts = useMemo(() => values.map((v, i) => [left + (i * (right - left)) / Math.max(1, values.length - 1), bottom - (v / max) * (bottom - top)] as [number, number]), [values.join(',')])
  const line = smooth(pts), area = line + ` L ${pts[pts.length - 1][0]} ${bottom} L ${pts[0][0]} ${bottom} Z`
  const pk = values.indexOf(max), P = pts[pk]
  const grid = [0, 0.25, 0.5, 0.75, 1]
  return (
    <svg width="100%" viewBox={`0 0 ${W} ${H}`} className="area" role="img" aria-label="График встреч по месяцам">
      <defs><linearGradient id={'ag' + id} x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="#ff701e" stopOpacity="0.28" /><stop offset="100%" stopColor="#ff701e" stopOpacity="0" /></linearGradient></defs>
      {grid.map(g => { const y = bottom - g * (bottom - top); return <g key={g}><line x1={left} y1={y} x2={right} y2={y} stroke="var(--line)" strokeWidth="1" />
        <text x="2" y={y + 3} fill="var(--ink-muted)" fontSize="8.5" fontWeight="500">{Math.round(g * max)}</text></g> })}
      <motion.path d={area} fill={`url(#ag${id})`} initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: 0.5, duration: 0.6 }} />
      <motion.path d={line} stroke="#ff701e" strokeWidth="3" strokeLinecap="round" fill="none" initial={{ pathLength: 0 }} animate={{ pathLength: 1 }} transition={{ duration: 1.2, ease: 'easeInOut' }} />
      {max > 0 && P && (
        <motion.g initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 1.1 }}>
          <g transform={`translate(${Math.min(Math.max(P[0], 50), right - 26)}, ${P[1] - 14})`}>
            <rect x="-26" y="-16" width="52" height="19" rx="9.5" fill="var(--surface)" stroke="#ff701e" strokeWidth="1.5" />
            <text x="0" y="-3" textAnchor="middle" fill="#ff701e" fontSize="9.5" fontWeight="700">{peakLabel ? peakLabel(max) : max + unit}</text>
          </g>
          <circle cx={P[0]} cy={P[1]} r="5" fill="var(--surface)" stroke="#ff701e" strokeWidth="3" />
        </motion.g>
      )}
      <g fill="var(--ink-muted)" fontSize="9" fontWeight="500">{labels.map((l, i) => <text key={i} x={pts[i][0]} y={H - 6} textAnchor="middle">{l}</text>)}</g>
    </svg>
  )
}

// ---------- month calendar ----------
const WD = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']
const MONTHS = ['Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь', 'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь']
const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

export type DayMark = { planned?: boolean; done?: boolean; deadline?: boolean; ends?: boolean }

export function MonthGrid({ month, marks, today, selected, onSelect, onMonth }: {
  month: Date; marks: Record<string, DayMark>; today: string; selected: string; onSelect: (d: string) => void; onMonth: (delta: number) => void
}) {
  const first = new Date(month.getFullYear(), month.getMonth(), 1)
  const lead = (first.getDay() + 6) % 7
  const days = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate()
  const cells: (Date | null)[] = [...Array(lead).fill(null), ...Array.from({ length: days }, (_, i) => new Date(month.getFullYear(), month.getMonth(), i + 1))]
  while (cells.length % 7) cells.push(null)
  return (
    <div className="mcal">
      <div className="mcal__head">
        <b>{MONTHS[month.getMonth()]} {month.getFullYear()}</b>
        <span className="spacer" />
        <button className="mcal__arrow" aria-label="Предыдущий месяц" onClick={() => onMonth(-1)}><Icon n="back" s={16} w={2.4} /></button>
        <button className="mcal__arrow" aria-label="Следующий месяц" onClick={() => onMonth(1)}><Icon n="chev" s={16} w={2.4} /></button>
      </div>
      <div className="mcal__grid">
        {WD.map(w => <span key={w} className="mcal__wd">{w}</span>)}
        {cells.map((d, i) => {
          if (!d) return <span key={i} />
          const k = ymd(d), m = marks[k] || {}
          const cls = ['mcal__day', m.planned && 'is-planned', m.done && 'is-done', k === today && 'is-today', k === selected && 'is-sel', k < today && 'is-past'].filter(Boolean).join(' ')
          return (
            <motion.button key={k} className={cls} whileTap={{ scale: 0.88 }} onClick={() => onSelect(k)} aria-label={k} aria-pressed={k === selected}>
              {d.getDate()}
              {(m.deadline || m.ends) && <i className={'mcal__dot' + (m.ends ? ' is-ends' : '')} />}
            </motion.button>
          )
        })}
      </div>
      <div className="mcal__legend"><span><i style={{ background: '#ff701e' }} />встреча</span><span><i style={{ background: '#22c55e' }} />были</span><span><i style={{ background: '#3b82f6' }} />дедлайн</span><span><i style={{ background: '#ff3b71' }} />сегодня</span></div>
    </div>
  )
}
