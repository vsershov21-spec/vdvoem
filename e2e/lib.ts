import { expect, devices, type Browser, type BrowserContext, type Page } from '@playwright/test'

export const EMU = process.env.EMU!
export const TOKEN = process.env.DEV_TOKEN!
export const BASE = process.env.BASE || 'http://localhost:18099'

export type U = { id: number; name: string }
// unique people per test: one database serves the whole run (random ids, never reused across files)
export const people = (a: string, b: string): [U, U] => { const n = 2 * (50_000 + Math.floor(Math.random() * 400_000_000)); return [{ id: n, name: a }, { id: n + 1, name: b }] }

export const shot = (p: Page, n: string) => p.screenshot({ path: `shots/${n}.png` })

// ---------- bot (through the emulator) ----------
type Msg = { message_id: number; text: string; method: string; file_name?: string; file?: string; reply_markup?: any; silent?: boolean }

export async function say(u: U, text: string) {
  const r = await fetch(EMU + '/_emu/say', { method: 'POST', body: JSON.stringify({ from: u.id, name: u.name, text }) })
  expect(r.ok, await r.text()).toBeTruthy()
}
export async function reply(u: U, to: number, text: string) {
  const r = await fetch(EMU + '/_emu/say', { method: 'POST', body: JSON.stringify({ from: u.id, name: u.name, text, reply: to }) })
  expect(r.ok, await r.text()).toBeTruthy()
}
export async function forward(u: U, channel: string, text: string) {
  const r = await fetch(EMU + '/_emu/say', { method: 'POST', body: JSON.stringify({ from: u.id, name: u.name, text, forward: channel }) })
  expect(r.ok, await r.text()).toBeTruthy()
}
export async function photo(u: U) {
  const r = await fetch(EMU + '/_emu/say', { method: 'POST', body: JSON.stringify({ from: u.id, name: u.name, photo: true }) })
  expect(r.ok).toBeTruthy()
}
export async function press(u: U, label: string) {
  const r = await fetch(EMU + '/_emu/press', { method: 'POST', body: JSON.stringify({ from: u.id, name: u.name, label }) })
  expect(r.ok, `button «${label}» for ${u.name}: ` + await r.text()).toBeTruthy()
}
export async function chat(u: U): Promise<Msg[]> { return (await fetch(EMU + '/_emu/chat/' + u.id)).json() }
export async function waitMsg(u: U, re: RegExp, timeout = 15000, after = 0): Promise<Msg> {
  const t0 = Date.now()
  while (Date.now() - t0 < timeout) {
    const m = (await chat(u)).find(x => x.message_id > after && re.test(x.text))
    if (m) return m
    await new Promise(r => setTimeout(r, 250))
  }
  throw new Error(`${u.name}: no message ${re}. Last: ` + JSON.stringify((await chat(u)).map(x => x.text.slice(0, 90)).slice(-6)))
}
export async function noMsg(u: U, re: RegExp, after = 0) {
  expect((await chat(u)).filter(x => x.message_id > after && re.test(x.text)), `${u.name} must not get ${re}`).toHaveLength(0)
}
export const lastId = async (u: U) => { const c = await chat(u); return c.length ? c[c.length - 1].message_id : 0 }

// /start → invite → partner joins
export async function pair(a: U, b: U) {
  await say(a, '/start')
  const inv = await waitMsg(a, /Пригласи партнёра/)
  const code = inv.text.match(/start=(inv_[A-Z0-9]+)/)![1]
  await say(b, '/start ' + code)
  await waitMsg(a, new RegExp(b.name + ' с тобой'))
  return code
}

// ---------- API as a person (dev header) ----------
export async function api<T = any>(u: U, method: string, path: string, body?: unknown): Promise<T> {
  const r = await fetch(BASE + '/api' + path, { method, headers: { 'Content-Type': 'application/json', 'X-Dev-Token': TOKEN, 'X-Dev-User': String(u.id), 'X-Dev-Name': encodeURIComponent(u.name) },
    body: body === undefined ? undefined : JSON.stringify(body) })
  const j = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(`${method} ${path} → ${r.status} ${JSON.stringify(j)}`)
  return j as T
}
export async function addIdeas(u: U, titles: string[]) {
  const ids: number[] = []
  for (const t of titles) { const r = await api(u, 'POST', '/ideas', { text: t }); ids.push((r.idea || r.existing).id) }
  return ids
}

// a planned meeting through the API: both mark the evening of the 2nd day, vote the favourite, confirm
export async function planMeeting(a: U, b: U) {
  const { round } = await api(a, 'POST', '/rounds')
  const k = round.days[1] + ':2'
  await api(a, 'POST', `/rounds/${round.id}/slots`, { slots: [k] })
  const v = (await api(b, 'POST', `/rounds/${round.id}/slots`, { slots: [k] })).round
  await api(a, 'POST', `/rounds/${round.id}/vote`, { idea_id: v.triple[0].id })
  await api(b, 'POST', `/rounds/${round.id}/vote`, { idea_id: v.triple[0].id })
  await api(a, 'POST', `/rounds/${round.id}/confirm`)
  return (await api(b, 'POST', `/rounds/${round.id}/confirm`)).round
}

// ---------- the e2e clock ----------
export async function advance(opt: { minutes?: number; to?: number }) {
  const r = await fetch(BASE + '/api/dev/advance', { method: 'POST', headers: { 'X-Dev-Token': TOKEN, 'Content-Type': 'application/json' },
    body: JSON.stringify(opt.to ? { to: new Date(opt.to * 1000).toISOString() } : { minutes: opt.minutes }) })
  expect(r.ok).toBeTruthy()
  return (await r.json()).now as string
}

// ---------- the Mini App ----------
export async function phone(browser: Browser, opts: { dark?: boolean; reduced?: boolean } = {}) {
  const ctx = await browser.newContext({ ...devices['iPhone 13'], baseURL: BASE, locale: 'ru-RU', timezoneId: 'Europe/Moscow',
    colorScheme: opts.dark ? 'dark' : 'light', reducedMotion: opts.reduced ? 'reduce' : 'no-preference' })
  return ctx
}
export async function open(ctx: BrowserContext, u: U | null, path = '', intro = false) {
  const p = await ctx.newPage()
  p.on('pageerror', e => console.log('pageerror:', e.message))
  p.on('popup', pp => { pp.close().catch(() => {}) }) // t.me links opened by the login screen
  const q = new URLSearchParams()
  if (u) { q.set('dev', `${u.id}:${u.name}`); q.set('devtoken', TOKEN) }
  if (!intro) q.set('nointro', '1')
  await p.goto(`/app/${path}?${q}`)
  if (u && !intro) await expect(p.locator('main.screen')).toBeVisible({ timeout: 10000 })
  return p
}
export const tab = (p: Page, id: 'home' | 'bank' | 'cal' | 'ai' | 'us') => p.locator('#tab-' + id).click()

// ---------- a fake Telegram client (served instead of telegram-web-app.js) ----------
// platform 'ios' + bio:true = an iPhone with Face ID; 'tdesktop' + bio:false = the desktop app (no sensor)
export async function fakeTelegram(ctx: BrowserContext, o: { platform: string; bio: boolean; authOk?: boolean }) {
  const js = `(() => {
    const ev = {}, cfg = ${JSON.stringify(o)}
    const root = document.documentElement
    const bm = { isInited: false, isBiometricAvailable: false, isAccessGranted: false, biometricType: 'unknown',
      init(cb) { setTimeout(() => { this.isInited = true; this.isBiometricAvailable = cfg.bio; this.biometricType = cfg.bio ? 'face' : 'unknown'; cb && cb() }, 30) },
      requestAccess(p, cb) { this.isAccessGranted = cfg.bio; window.__bio = (window.__bio || []).concat('request'); setTimeout(() => cb(cfg.bio), 10) },
      authenticate(p, cb) { window.__bio = (window.__bio || []).concat('auth'); setTimeout(() => cb(!!cfg.authOk), 10) } }
    window.Telegram = { WebApp: {
      initData: 'query_id=e2e&user=%7B%22id%22%3A1%7D&auth_date=1&hash=e2e', initDataUnsafe: {}, version: '8.0', platform: cfg.platform,
      colorScheme: 'light', themeParams: {}, isFullscreen: false, isExpanded: true,
      isVersionAtLeast: v => parseFloat(v) <= 8.0,
      ready() {}, expand() {}, disableVerticalSwipes() {}, setHeaderColor() {}, setBackgroundColor() {}, setBottomBarColor() {},
      requestFullscreen() { this.isFullscreen = true; root.style.setProperty('--tg-safe-area-inset-top', '47px'); root.style.setProperty('--tg-content-safe-area-inset-top', '46px'); window.__fullscreen = true },
      onEvent(n, f) { (ev[n] ||= []).push(f) }, offEvent() {},
      BackButton: { show() {}, hide() {}, onClick() {}, offClick() {} },
      HapticFeedback: { impactOccurred() {}, selectionChanged() {}, notificationOccurred() {} },
      BiometricManager: bm, openLink(u) { window.open(u) }, openTelegramLink(u) { window.open(u) },
    } }
  })()`
  await ctx.route('**/telegram-web-app.js*', r => r.fulfill({ contentType: 'application/javascript', body: js }))
}
