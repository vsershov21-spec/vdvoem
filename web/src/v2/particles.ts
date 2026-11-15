// Dots that fly in and settle into a shape: a heart («Совпало») or two rings (the loading mark).
// One burst of ≈1.6 s; the loop stops when the dots have landed, and with «reduce motion» they are simply there.
export const still = () => window.matchMedia('(prefers-reduced-motion:reduce)').matches

export function particles(cv: HTMLCanvasElement, shape: 'heart' | 'rings', n = 1500) {
  const g = cv.getContext('2d')
  if (!g) return
  const W = cv.width, H = cv.height
  const P: { tx: number; ty: number; sx: number; sy: number; dl: number; b: number; c: boolean }[] = []
  for (let k = 0; k < n; k++) {
    let x: number, y: number
    if (shape === 'rings') {
      const t = Math.random() * Math.PI * 2, r = (k % 5 ? 46 + (Math.random() - .5) * 7 : Math.sqrt(Math.random()) * 46) * 1.9, c = k % 2 ? -1 : 1
      x = c * 50 + Math.cos(t) * r; y = Math.sin(t) * r + 10
    } else {
      const t = Math.random() * Math.PI * 2, r = k < n * .6 ? 1 : Math.sqrt(Math.random())
      x = 16 * Math.sin(t) ** 3 * r * 8.6
      y = -(13 * Math.cos(t) - 5 * Math.cos(2 * t) - 2 * Math.cos(3 * t) - Math.cos(4 * t)) * r * 8.6
    }
    const a = Math.random() * Math.PI * 2, d = 160 + Math.random() * 220
    P.push({ tx: W / 2 + x, ty: H / 2 - 12 + y, sx: W / 2 + Math.cos(a) * d, sy: H / 2 + Math.sin(a) * d, dl: Math.random() * .35, b: .45 + Math.random() * .55, c: shape === 'rings' && k % 2 === 1 })
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
      g.globalAlpha = p.b * (.25 + .75 * u)
      g.fillRect(p.sx + (p.tx - p.sx) * u, p.sy + (p.ty - p.sy) * u, 3, 3)
    }
    if (T < 1.7) requestAnimationFrame(frame)
  }
  requestAnimationFrame(frame)
}
