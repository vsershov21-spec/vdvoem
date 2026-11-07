// Animated heart — ported from svg_animations/HeartIcon: lub-dub beat, gradient fills bottom→top, ECG line, aura, particles.
// Recoloured to the brand (ember → rose, with a blue spark for the partner). Used for «Очень хочу» and «Понравилось».
import { useId } from 'react'
import { motion } from 'framer-motion'

const parts = [
  { cx: 6, cy: 7, d: 0.15, heart: true, c: '#ef6537', dx: -4, dy: -10 },
  { cx: 9, cy: 4, d: 0.18, heart: false, c: '#4d7dff', dx: -2, dy: -12 },
  { cx: 12, cy: 3, d: 0.22, heart: true, c: '#ff5a7a', dx: 0, dy: -14 },
  { cx: 15, cy: 4, d: 0.16, heart: false, c: '#ffb38a', dx: 2, dy: -12 },
  { cx: 18, cy: 7, d: 0.2, heart: true, c: '#e04d1d', dx: 4, dy: -10 },
]
const PATH = 'M19.5 12.572l-7.5 7.428l-7.5 -7.428a5 5 0 1 1 7.5 -6.566a5 5 0 1 1 7.5 6.572 Z'

export function Heart({ on, size = 24, stroke = 1.6, color = 'currentColor', burst = true }: { on: boolean; size?: number; stroke?: number; color?: string; burst?: boolean }) {
  const id = useId().replace(/:/g, '')
  const st = on ? 'on' : 'off'
  return (
    <motion.svg width={size} height={size} viewBox="-4 -4 32 32" fill="none" stroke={color} strokeWidth={stroke} strokeLinecap="round" strokeLinejoin="round"
      initial={false} animate={st} style={{ overflow: 'visible', flex: 'none' }} aria-hidden="true">
      <defs>
        <linearGradient id={'hg' + id} x1="0%" y1="100%" x2="0%" y2="0%">
          <stop offset="0%" stopColor="#d9381e" /><stop offset="50%" stopColor="#ef6537" /><stop offset="100%" stopColor="#ff7a8a" />
        </linearGradient>
        <radialGradient id={'ha' + id} cx="50%" cy="50%" r="50%">
          <stop offset="0%" stopColor="#ef6537" stopOpacity="0.6" /><stop offset="55%" stopColor="#ff5a7a" stopOpacity="0.25" /><stop offset="100%" stopColor="#ff5a7a" stopOpacity="0" />
        </radialGradient>
        <clipPath id={'hc' + id}><path d={PATH} /></clipPath>
      </defs>
      {burst && <motion.circle cx="12" cy="12" r="11" fill={`url(#ha${id})`} stroke="none"
        variants={{ off: { opacity: 0, scale: 0.5 }, on: { opacity: [0, 0.75, 0.35], scale: [0.5, 1.25, 1], transition: { duration: 0.4, delay: 0.1 } } }} />}
      {burst && <motion.path d="M -2 13 L 4 13 L 7 8 L 10 18 L 13 6 L 16 15 L 19 13 L 26 13" stroke="#ef6537" strokeWidth={stroke * 0.85}
        variants={{ off: { pathLength: 0, opacity: 0 }, on: { pathLength: [0, 1], opacity: [0, 0.75, 0], transition: { duration: 0.4, delay: 0.03 } } }} />}
      <motion.g style={{ originX: '12px', originY: '13px' }}
        variants={{ off: { scale: 1, transition: { duration: 0.25 } }, on: { scale: [1, 1.16, 1.02, 1.08, 1], transition: { duration: 0.5 } } }}>
        <g clipPath={`url(#hc${id})`}>
          <motion.rect x="2" y="2" width="20" height="20" fill={`url(#hg${id})`} stroke="none"
            variants={{ off: { y: 26, opacity: 0, transition: { duration: 0.25 } }, on: { y: 0, opacity: 1, transition: { duration: 0.4, delay: 0.06 } } }} />
        </g>
        <motion.path d={PATH} variants={{ off: { stroke: color }, on: { stroke: '#e04d1d' } }} />
      </motion.g>
      {burst && parts.map((p, i) => (
        <motion.g key={i} variants={{ off: { opacity: 0, scale: 0, x: 0, y: 0 }, on: { opacity: [0, 1, 0], scale: [0, 1.3, 0], x: [0, p.dx / 2, p.dx], y: [0, p.dy / 2, p.dy], transition: { duration: 0.45, delay: p.d } } }}>
          {p.heart ? <path d={`M${p.cx} ${p.cy + 1.2}l-1.2 -1.2a.8 .8 0 1 1 1.2 -1a.8 .8 0 1 1 1.2 1z`} fill={p.c} stroke="none" />
            : <circle cx={p.cx} cy={p.cy} r="0.9" fill={p.c} stroke="none" />}
        </motion.g>
      ))}
    </motion.svg>
  )
}
