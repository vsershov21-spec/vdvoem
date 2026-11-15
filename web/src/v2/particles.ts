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
