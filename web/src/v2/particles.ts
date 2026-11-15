// Dots that fly in and settle into a shape: a heart («Совпало») or the mark, an arch of two halves (the loading screen).
// One burst of ≈1.6 s; the loop stops when the dots have landed, and with «reduce motion» they are simply there.
export const still = () => window.matchMedia('(prefers-reduced-motion:reduce)').matches

// The arch of the mark (public/mark.svg): two strokes that start apart and meet at the top
const arch = (left: boolean, t: number): [number, number] => {
  const x0 = left ? 8 : 76, x1 = left ? 22 : 62, u = 1 - t
  return [u * u * x0 + 2 * u * t * x1 + t * t * 42 - 42, u * u * 46 + 2 * u * t * 10 + t * t * 10 - 28]
}

// fill: the canvas takes the whole of its box and the dots start scattered all over it (the loading screen)
export function particles(cv: HTMLCanvasElement, shape: 'heart' | 'arch', n = 1500, fill = false) {
  const g = cv.getContext('2d')
  if (!g) return
  const dpr = fill ? Math.min(window.devicePixelRatio || 1, 2) : 1
  if (fill) { cv.width = Math.round(cv.clientWidth * dpr); cv.height = Math.round(cv.clientHeight * dpr) }
  const W = cv.width, H = cv.height
  const P: { tx: number; ty: number; sx: number; sy: number; dl: number; b: number; c: boolean }[] = []
  for (let k = 0; k < n; k++) {
    let x: number, y: number
    if (shape === 'arch') {
      const [ax, ay] = arch(k % 2 === 0, Math.random()), a = Math.random() * Math.PI * 2, r = Math.sqrt(Math.random()) * 4.5, z = (fill ? 2.5 : 3.2) * dpr
      x = (ax + Math.cos(a) * r) * z; y = (ay + Math.sin(a) * r) * z + 12
    } else {
      const t = Math.random() * Math.PI * 2, r = k < n * .6 ? 1 : Math.sqrt(Math.random())
      x = 16 * Math.sin(t) ** 3 * r * 8.6
      y = -(13 * Math.cos(t) - 5 * Math.cos(2 * t) - 2 * Math.cos(3 * t) - Math.cos(4 * t)) * r * 8.6
    }
    const a = Math.random() * Math.PI * 2, d = 160 + Math.random() * 220
    P.push({ tx: W / 2 + x, ty: H / 2 - 12 + y, sx: fill ? Math.random() * W : W / 2 + Math.cos(a) * d, sy: fill ? Math.random() * H : H / 2 + Math.sin(a) * d, dl: Math.random() * .35, b: .45 + Math.random() * .55, c: shape === 'arch' && k % 2 === 1 })
  }
  const cs = getComputedStyle(cv), col = cs.getPropertyValue('--accent').trim() || '#ef6537', col2 = cs.getPropertyValue('--blue').trim() || '#3d6ef5'
  const t0 = performance.now()
  const frame = (now: number) => {
    if (!cv.isConnected) return
    const T = still() ? 2 : (now - t0) / 1000
    g.clearRect(0, 0, W, H)
    for (const p of P) {
      let u = Math.min(Math.max((T - p.dl) / 1.25, 0), 1)
      u = u < .5 ? 4 * u * u * u : 1 - (-2 * u + 2) ** 3 / 2
      g.fillStyle = p.c ? col2 : col
      g.globalAlpha = p.b * ((fill ? .5 : .25) + (fill ? .5 : .75) * u)
      g.fillRect(p.sx + (p.tx - p.sx) * u, p.sy + (p.ty - p.sy) * u, 3 * dpr, 3 * dpr)
    }
    if (T < 1.7) requestAnimationFrame(frame)
  }
  requestAnimationFrame(frame)
}

// ---------- miniatures: a small living picture of dots (empty states). The same language as the loading screen:
// dots gather into a shape, then breathe; a wave of light runs through them every few seconds. ----------
export type MiniShape = 'arch' | 'heart' | 'check' | 'cal' | 'spark' | 'search'
// a point of the shape in a box of about ±50 × ±30, and which half it belongs to (the two colours of the mark)
const SHAPES: Record<MiniShape, (k: number, n: number) => [number, number, boolean]> = {
  arch: k => { const l = k % 2 === 0, [x, y] = arch(l, Math.random()); return [x * 1.5, y * 1.5 + 4, !l] },
  heart: (k, n) => {
    const t = Math.random() * Math.PI * 2, r = k < n * .7 ? 1 : Math.sqrt(Math.random())
    const x = 16 * Math.sin(t) ** 3 * r * 1.9, y = -(13 * Math.cos(t) - 5 * Math.cos(2 * t) - 2 * Math.cos(3 * t) - Math.cos(4 * t)) * r * 1.9 - 2
    return [x, y, x > 0]
  },
  check: k => { // a ring and a tick inside it
    if (k % 3) { const t = Math.random() * Math.PI * 2; return [Math.cos(t) * 29, Math.sin(t) * 29, Math.cos(t) > 0] }
    const u = Math.random(), a = u < .36
    return a ? [-13 + (u / .36) * 10, 0 + (u / .36) * 11, false] : [-3 + ((u - .36) / .64) * 18, 11 - ((u - .36) / .64) * 22, true]
  },
  cal: k => { // a month: seven columns of days, two of them are «ours»
    const c = k % 7, r = Math.floor(k / 7) % 4, hot = (c === 2 && r === 1) || (c === 5 && r === 2), j = hot ? 3.4 : 1.1
    return [(c - 3) * 13 + (Math.random() - .5) * j * 2, (r - 1.5) * 13 + (Math.random() - .5) * j * 2, hot ? c === 5 : c > 3]
  },
  spark: k => { // the four-pointed star of the assistant, a small one beside it
    const t = Math.random() * Math.PI * 2, small = k % 5 === 0, s = small ? 9 : 27
    const x = Math.cos(t) ** 3 * s + (small ? 30 : -6), y = Math.sin(t) ** 3 * s + (small ? -18 : 2)
    return [x, y, small || x > -6]
  },
  search: k => { // a lens and its handle: nothing was found
    if (k % 4 === 0) { const u = Math.random(); return [14 + u * 15, 12 + u * 15, true] }
    const t = Math.random() * Math.PI * 2
    return [Math.cos(t) * 20 - 4, Math.sin(t) * 20 - 6, false]
  },
}

export function mini(cv: HTMLCanvasElement, shape: MiniShape, n = 360): () => void {
  const g = cv.getContext('2d')
  if (!g) return () => {}
  const dpr = Math.min(window.devicePixelRatio || 1, 2), cw = cv.clientWidth || 180, ch = cv.clientHeight || 96
  cv.width = Math.round(cw * dpr); cv.height = Math.round(ch * dpr)
  const W = cv.width, H = cv.height, z = (ch / 78) * dpr
  const P = Array.from({ length: n }, (_, k) => {
    const [x, y, blue] = SHAPES[shape](k, n), a = Math.random() * Math.PI * 2, d = 30 + Math.random() * 70
    return { x: W / 2 + x * z, y: H / 2 + y * z, sx: W / 2 + (x + Math.cos(a) * d) * z, sy: H / 2 + (y + Math.sin(a) * d) * z, blue,
      dl: Math.random() * .3, ph: Math.random() * Math.PI * 2, w: .6 + Math.random() * .9, b: .5 + Math.random() * .5, r: Math.hypot(x, y) }
  })
  const cs = getComputedStyle(cv), col = cs.getPropertyValue('--accent').trim() || '#ef6537', col2 = cs.getPropertyValue('--blue-soft').trim() || cs.getPropertyValue('--blue').trim() || '#3d6ef5'
  const quiet = still(), t0 = performance.now()
  let raf = 0, seen = true
  const io = 'IntersectionObserver' in window ? new IntersectionObserver(e => { seen = e[0].isIntersecting; if (seen && !raf && !quiet) raf = requestAnimationFrame(frame) }) : null
  io?.observe(cv)
  function frame(now: number) {
    raf = 0
    if (!cv.isConnected) { io?.disconnect(); return }
    const T = quiet ? 9 : (now - t0) / 1000, wave = ((T - 1.4) % 4.2) * 46 // the light: a ring that grows from the middle
    g!.clearRect(0, 0, W, H)
    for (const p of P) {
      let u = Math.min(Math.max((T - p.dl) / .95, 0), 1)
      u = 1 - (1 - u) ** 3
      const lit = T > 1.4 ? Math.max(0, 1 - Math.abs(p.r - wave) / 9) : 0
      const ox = quiet ? 0 : Math.sin(T * p.w + p.ph) * 1.1 * dpr * u, oy = quiet ? 0 : Math.cos(T * p.w * .8 + p.ph) * 1.1 * dpr * u
      const s = (1.7 + lit * 1.3) * dpr
      g!.globalAlpha = Math.min(1, p.b * (.2 + .8 * u) * (.72 + .28 * Math.sin(T * 1.7 + p.ph)) + lit * .6)
      g!.fillStyle = p.blue ? col2 : col
      g!.beginPath(); g!.arc(p.sx + (p.x - p.sx) * u + ox, p.sy + (p.y - p.sy) * u + oy, s / 2, 0, 6.2832); g!.fill()
    }
    if (!quiet && seen && !document.hidden) raf = requestAnimationFrame(frame)
  }
  const vis = () => { if (!document.hidden && seen && !raf && !quiet) raf = requestAnimationFrame(frame) }
  document.addEventListener('visibilitychange', vis)
  raf = requestAnimationFrame(frame)
  return () => { cancelAnimationFrame(raf); io?.disconnect(); document.removeEventListener('visibilitychange', vis) }
}
