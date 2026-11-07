// Particle morph — ported from the particleAnimation donor (Canvas 2D, burst → gather, pointer scatter, reduced-motion aware).
// Added: the «pair» shape (our mark: two discs, ember + blue, light where they overlap) and per-particle colour.
import { useEffect, useRef } from 'react'

export type Shape = 'sphere' | 'pair' | 'heart' | 'ring' | `text:${string}`

const SPEED = 0.28, BASE_TILT = 0.18, REPEL_RADIUS = 0.6, REPEL_FORCE = 4, SPRING = 0.04, DAMPING = 0.84
const SCATTER = 0.7, FORM = 1.6, MAX_DELAY = 0.4
const isFlat = (id: Shape) => id === 'heart' || id === 'pair' || id.startsWith('text:')

// colours: 0 white, 1 ember (person a), 2 blue (person b), 3 warm light
const PALETTE = [[255, 255, 255], [239, 101, 55], [77, 125, 255], [255, 222, 150]]

type Built = { pos: Float32Array; col?: Uint8Array }

function sphere(n: number): Built {
  const out = new Float32Array(n * 3)
  for (let i = 0; i < n; i++) {
    let y = Math.random() < 0.28 ? (Math.random() < 0.5 ? -1 : 1) * (1 - Math.pow(Math.random(), 2.2) * 0.35) : Math.random() * 2 - 1
    const a = Math.random() * Math.PI * 2, ring = Math.sqrt(1 - y * y), r = 1 + (Math.random() - 0.5) * 0.06
    out[i * 3] = Math.cos(a) * ring * r; out[i * 3 + 1] = y * r; out[i * 3 + 2] = Math.sin(a) * ring * r
  }
  return { pos: out }
}

function ring(n: number): Built {
  const out = new Float32Array(n * 3)
  for (let i = 0; i < n; i++) {
    const u = Math.random() * Math.PI * 2, v = Math.random() * Math.PI * 2, tube = 0.16 * Math.sqrt(Math.random()), k = i * 3
    const x = (1 + tube * Math.cos(v)) * Math.cos(u), y = tube * Math.sin(v), z = (1 + tube * Math.cos(v)) * Math.sin(u)
    out[k] = x; out[k + 1] = y * Math.cos(1.15) - z * Math.sin(1.15); out[k + 2] = y * Math.sin(1.15) + z * Math.cos(1.15)
  }
  return { pos: out }
}

function heart(n: number): Built {
  const out = new Float32Array(n * 3), col = new Uint8Array(n)
  for (let i = 0; i < n; i++) {
    const t = Math.random() * Math.PI * 2
    const k = Math.random() < 0.6 ? 0.88 + Math.random() * 0.12 : Math.sqrt(Math.random())
    const hx = 16 * Math.sin(t) ** 3, hy = 13 * Math.cos(t) - 5 * Math.cos(2 * t) - 2 * Math.cos(3 * t) - Math.cos(4 * t)
    out[i * 3] = (hx / 17) * k * 1.1; out[i * 3 + 1] = (-hy / 17) * k * 1.1 - 0.15; out[i * 3 + 2] = (Math.random() * 2 - 1) * 0.28 * Math.sqrt(1 - k * 0.9)
    col[i] = out[i * 3] < -0.08 ? 1 : out[i * 3] > 0.08 ? 2 : 0
  }
  return { pos: out, col }
}

// the mark: two discs; dense rims, softer fill; overlap lens lit white
function pair(n: number): Built {
  const out = new Float32Array(n * 3), col = new Uint8Array(n)
  const R = 0.62, d = 0.31
  for (let i = 0; i < n; i++) {
    const side = i % 2 ? 1 : -1
    const a = Math.random() * Math.PI * 2
    const k = Math.random() < 0.7 ? 0.95 + Math.random() * 0.05 : Math.sqrt(Math.random()) * 0.95
    const x = side * d + Math.cos(a) * R * k, y = Math.sin(a) * R * k
    out[i * 3] = x; out[i * 3 + 1] = y; out[i * 3 + 2] = (Math.random() * 2 - 1) * 0.05
    const inA = (x + d) ** 2 + y * y < R * R, inB = (x - d) ** 2 + y * y < R * R
    col[i] = inA && inB ? 0 : side < 0 ? 1 : 2
  }
  return { pos: out, col }
}

function text(n: number, s: string): Built {
  const font = '800 200px Onest, "Helvetica Neue", system-ui, sans-serif'
  const c = document.createElement('canvas'), g = c.getContext('2d', { willReadFrequently: true })!
  g.font = font
  const tw = Math.ceil(g.measureText(s).width) + 40
  c.width = tw; c.height = 280; g.font = font; g.fillStyle = '#fff'; g.textAlign = 'center'; g.textBaseline = 'middle'; g.fillText(s, tw / 2, 140)
  const data = g.getImageData(0, 0, c.width, c.height).data, filled: number[] = []
  let minX = 1e9, maxX = -1e9, minY = 1e9, maxY = -1e9
  for (let y = 0; y < c.height; y += 2) for (let x = 0; x < c.width; x += 2) if (data[(y * c.width + x) * 4 + 3] > 128) {
    filled.push(x, y); minX = Math.min(minX, x); maxX = Math.max(maxX, x); minY = Math.min(minY, y); maxY = Math.max(maxY, y)
  }
  if (!filled.length) return sphere(n)
  const out = new Float32Array(n * 3), midX = (minX + maxX) / 2, midY = (minY + maxY) / 2
  const unit = Math.max((maxX - minX) / 2.4, (maxY - minY) / 1.2), m = filled.length / 2
  for (let i = 0; i < n; i++) {
    const j = Math.floor(Math.random() * m) * 2
    out[i * 3] = (filled[j] + Math.random() * 2 - midX) / unit; out[i * 3 + 1] = (filled[j + 1] + Math.random() * 2 - midY) / unit; out[i * 3 + 2] = (Math.random() * 2 - 1) * 0.06
  }
  return { pos: out }
}

function build(id: Shape, n: number): Built {
  if (id === 'ring') return ring(n)
  if (id === 'heart') return heart(n)
  if (id === 'pair') return pair(n)
  if (id.startsWith('text:')) return text(n, id.slice(5))
  return sphere(n)
}

const easeOut = (t: number) => 1 - (1 - t) ** 3
const easeInOut = (t: number) => (t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2)

export function Particles({ shape, count = 5200, scale = 0.24, from, colored = true, dissolve = false, className, style, interactive = true, speed = 1, onFormed }: {
  shape: Shape; count?: number; speed?: number; onFormed?: (s: Shape) => void; scale?: number; from?: Shape; colored?: boolean; dissolve?: boolean
  className?: string; style?: React.CSSProperties; interactive?: boolean
}) {
  const ref = useRef<HTMLCanvasElement>(null)
  const morph = useRef<(s: Shape) => void>()
  const formed = useRef(onFormed); formed.current = onFormed
  const cur = useRef<Shape>(from || shape)
  const out = useRef<() => void>()

  useEffect(() => {
    const canvas = ref.current!, ctx = canvas.getContext('2d')!
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    const N = reduce ? Math.min(count, 1800) : count
    const first = build(from || shape, N)
    const pos = first.pos, start = new Float32Array(N * 3), mid = new Float32Array(N * 3)
    let end = pos
    const col = new Uint8Array(N), colTo = first.col ? Uint8Array.from(first.col) : new Uint8Array(N)
    col.set(colTo)
    const bright = new Float32Array(N), delay = new Float32Array(N)
    for (let i = 0; i < N; i++) bright[i] = 0.35 + Math.random() * 0.65
    let morphT = -1, flatT = isFlat(from || shape) ? 1 : 0, flat = flatT, fade = 1, fadeTo = 1

    morph.current = (id: Shape) => {
      cur.current = id
      start.set(pos)
      const b = build(id, N); end = b.pos
      colTo.set(b.col || new Uint8Array(N))
      for (let i = 0; i < N; i++) {
        const k = i * 3, push = 1.5 + Math.random() * 0.6, th = Math.random() * Math.PI * 2, ph = Math.acos(Math.random() * 2 - 1), kick = 0.35 + Math.random() * 0.35
        mid[k] = start[k] * push + Math.sin(ph) * Math.cos(th) * kick
        mid[k + 1] = start[k + 1] * push + Math.sin(ph) * Math.sin(th) * kick
        mid[k + 2] = start[k + 2] * push + Math.cos(ph) * kick
        delay[i] = Math.random() * MAX_DELAY
      }
      morphT = 0; flatT = isFlat(id) ? 1 : 0
    }
    // dissolve: every particle flies outward and fades — the hand-off to the app
    out.current = () => {
      start.set(pos)
      for (let i = 0; i < N; i++) { const k = i * 3, p = 3 + Math.random() * 3; mid[k] = start[k] * p; mid[k + 1] = start[k + 1] * p + (Math.random() - .5); mid[k + 2] = start[k + 2] * p; delay[i] = 0 }
      end = mid; morphT = 0; fadeTo = 0
    }

    let w = 0, h = 0, dpr = 1
    const resize = () => { dpr = Math.min(window.devicePixelRatio || 1, 2); w = canvas.clientWidth * dpr; h = canvas.clientHeight * dpr; canvas.width = w; canvas.height = h }
    resize()
    const ro = new ResizeObserver(resize); ro.observe(canvas)
    const target = { x: 0, y: 0 }, tilt = { x: 0, y: 0 }
    const offset = new Float32Array(N * 2), vel = new Float32Array(N * 2), mouse = { x: -1e6, y: -1e6 }
    const onMove = (e: PointerEvent) => {
      if (!interactive) return
      target.x = (e.clientY / innerHeight - 0.5) * 0.5; target.y = (e.clientX / innerWidth - 0.5) * 0.7
      const r = canvas.getBoundingClientRect(); mouse.x = (e.clientX - r.left) * dpr; mouse.y = (e.clientY - r.top) * dpr
    }
    const onLeave = () => { mouse.x = -1e6; mouse.y = -1e6 }
    addEventListener('pointermove', onMove); addEventListener('pointerup', onLeave); addEventListener('blur', onLeave)

    let angle = 0, last = performance.now(), raf = 0
    const draw = (now: number) => {
      const raw = Math.min((now - last) / 1000, 0.25), dt = Math.min(raw, 0.05); last = now
      tilt.x += (target.x - tilt.x) * 0.05; tilt.y += (target.y - tilt.y) * 0.05; flat += (flatT - flat) * 0.04; fade += (fadeTo - fade) * 0.05
      if (flatT) { const home = Math.round(angle / (Math.PI * 2)) * Math.PI * 2; angle += (home - angle) * 0.05 } else if (!reduce) angle += dt * SPEED
      const sway = Math.sin(now * 0.0008) * 0.22 * flat
      if (morphT >= 0) {
        morphT += reduce ? raw * 4 : raw * speed
        if (morphT < SCATTER) {
          const e = easeOut(morphT / SCATTER)
          for (let k = 0; k < N * 3; k++) pos[k] = start[k] + (mid[k] - start[k]) * e
          if (morphT > SCATTER * 0.6) col.set(colTo)
        } else if (morphT < SCATTER + FORM + MAX_DELAY) {
          for (let i = 0; i < N; i++) {
            const e = easeInOut(Math.min(Math.max((morphT - SCATTER - delay[i]) / FORM, 0), 1)), k = i * 3
            pos[k] = mid[k] + (end[k] - mid[k]) * e; pos[k + 1] = mid[k + 1] + (end[k + 1] - mid[k + 1]) * e; pos[k + 2] = mid[k + 2] + (end[k + 2] - mid[k + 2]) * e
          }
        } else { pos.set(end); morphT = -1; if (fadeTo > 0) formed.current?.(cur.current) }
      }
      ctx.clearRect(0, 0, w, h)
      ctx.globalCompositeOperation = 'lighter'
      const R = Math.min(w, h) * scale, cx = w / 2, cy = h / 2
      const yaw = angle + tilt.y + sway, pitch = BASE_TILT * (1 - flat) + tilt.x
      const cA = Math.cos(yaw), sA = Math.sin(yaw), cT = Math.cos(pitch), sT = Math.sin(pitch), fov = 3.2, step = dt * 60
      const rr = R * REPEL_RADIUS, rr2 = rr * rr
      let curCol = -1
      for (let i = 0; i < N; i++) {
        const x = pos[i * 3], y = pos[i * 3 + 1], z = pos[i * 3 + 2]
        const x1 = x * cA + z * sA, z1 = -x * sA + z * cA, y2 = y * cT - z1 * sT, z2 = y * sT + z1 * cT
        const p = fov / Math.max(fov - z2, 0.5), depth = Math.min(Math.max((z2 + 1) / 2, 0), 1)
        const hx = cx + x1 * R * p, hy = cy + y2 * R * p, o = i * 2
        let vx = vel[o], vy = vel[o + 1]
        const dx = hx + offset[o] - mouse.x, dy = hy + offset[o + 1] - mouse.y, d2 = dx * dx + dy * dy
        if (d2 < rr2 && d2 > 0.01) { const d = Math.sqrt(d2), f = (1 - d / rr) ** 2 * REPEL_FORCE * dpr * (0.4 + 0.6 * depth); vx += (dx / d) * f * step; vy += (dy / d) * f * step }
        vx = (vx - offset[o] * SPRING * step) * DAMPING ** step; vy = (vy - offset[o + 1] * SPRING * step) * DAMPING ** step
        offset[o] += vx * step; offset[o + 1] += vy * step; vel[o] = vx; vel[o + 1] = vy
        const c = colored ? col[i] : 0
        if (c !== curCol) { const q = PALETTE[c]; ctx.fillStyle = `rgb(${q[0]},${q[1]},${q[2]})`; curCol = c }
        const size = (1 + depth * 1.3) * dpr
        ctx.globalAlpha = bright[i] * (0.22 + 0.78 * depth) * fade
        ctx.fillRect(hx + offset[o] - size / 2, hy + offset[o + 1] - size / 2, size, size)
      }
      ctx.globalAlpha = 1; ctx.globalCompositeOperation = 'source-over'
      raf = requestAnimationFrame(draw)
    }
    raf = requestAnimationFrame(draw)
    if (from && from !== shape) setTimeout(() => morph.current?.(shape), 60)
    return () => { cancelAnimationFrame(raf); ro.disconnect(); removeEventListener('pointermove', onMove); removeEventListener('pointerup', onLeave); removeEventListener('blur', onLeave) }
  }, [])

  const prev = useRef(shape)
  useEffect(() => { if (prev.current !== shape) { prev.current = shape; morph.current?.(shape) } }, [shape])
  useEffect(() => { if (dissolve) out.current?.() }, [dissolve])

  return <canvas ref={ref} className={className} style={{ width: '100%', height: '100%', display: 'block', ...style }} role="img" aria-label="Анимация" />
}
