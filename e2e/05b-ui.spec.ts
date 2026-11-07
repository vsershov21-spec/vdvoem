import { test, expect, type Page } from '@playwright/test'
import { people, pair, phone, open, shot, api, addIdeas, planMeeting, tab, fakeTelegram } from './lib'

// Layout shifts (CLS) are collected from the first paint; inputs must not trigger iOS zoom; nothing scrolls sideways;
// the tab bar and the island stay put between tabs.
const probe = () => {
  ;(window as any).__cls = 0
  ;(window as any).__shifts = []
  new PerformanceObserver(l => {
    for (const e of l.getEntries() as any[]) {
      if (e.hadRecentInput) continue
      ;(window as any).__cls += e.value
      ;(window as any).__shifts.push({ v: +e.value.toFixed(4), n: (e.sources || []).map((s: any) => s.node?.className || s.node?.nodeName).slice(0, 3) })
    }
  }).observe({ type: 'layout-shift', buffered: true })
}

async function settle(p: Page) { await p.waitForLoadState('networkidle'); await p.waitForTimeout(900) }
async function audit(p: Page, name: string) {
  await settle(p)
  const r = await p.evaluate(() => {
    const vw = innerWidth
    const wide = [...document.querySelectorAll<HTMLElement>('body *')].filter(e => { const b = e.getBoundingClientRect(); return b.width > 0 && b.right > vw + 1 && getComputedStyle(e).position !== 'fixed' && !e.closest('.wkchips,.hscroll,.chips,.feed,[data-scroll-x]') })
      .slice(0, 5).map(e => e.className || e.nodeName)
    const small = [...document.querySelectorAll<HTMLInputElement>('input:not([type=checkbox]):not([type=hidden]),textarea')].filter(e => e.offsetParent && parseFloat(getComputedStyle(e).fontSize) < 16).map(e => e.className || e.name)
    const tiny = [...document.querySelectorAll<HTMLElement>('button,a[href],[role=tab]')].filter(e => { const b = e.getBoundingClientRect(); return e.offsetParent && b.width > 0 && (b.width < 24 || b.height < 24) && getComputedStyle(e).visibility !== 'hidden' })
      .slice(0, 5).map(e => (e.getAttribute('aria-label') || e.textContent || e.className).slice(0, 30))
    return { scroll: document.documentElement.scrollWidth - vw, wide, small, tiny, cls: (window as any).__cls as number, shifts: (window as any).__shifts }
  })
  await shot(p, 'E26-' + name)
  expect.soft(r.scroll, `${name}: page scrolls sideways (${r.wide.join(', ')})`).toBeLessThanOrEqual(1)
  expect.soft(r.small, `${name}: inputs under 16px zoom on iPhone`).toEqual([])
  expect.soft(r.tiny, `${name}: tap targets under 24px`).toEqual([])
  expect.soft(r.cls, `${name}: layout jumps ${JSON.stringify(r.shifts)}`).toBeLessThan(0.1)
  await p.evaluate(() => { (window as any).__cls = 0; (window as any).__shifts = [] })
}

for (const dark of [false, true]) {
  test(`E26 интерфейс на iPhone (${dark ? 'тёмная' : 'светлая'}): без скачков, без бокового скролла, всё нажимается`, async ({ browser }) => {
    test.setTimeout(120_000)
    const [v, n] = people('Вова', 'Настя')
    await pair(v, n)
    const ids = await addIdeas(v, ['Ужин в грузинском ресторане', 'Театр на Таганке', 'Прогулка по набережной', 'Каток в парке', 'Помыть окна'])
    await api(v, 'POST', `/ideas/${ids[0]}/wish`, { wish: 'very' })
    await api(n, 'POST', `/ideas/${ids[0]}/wish`, { wish: 'want' })
    await planMeeting(v, n)
    const ctx = await phone(browser, { dark })
    await fakeTelegram(ctx, { platform: 'ios', bio: false }) // fullscreen insets like a real iPhone in Telegram
    await ctx.addInitScript(probe)
    const s = dark ? '-dark' : ''
    const p = await open(ctx, v)
    await audit(p, 'home' + s)
    const nav = await p.locator('.pnav').boundingBox()
    const island = await p.getByTestId('island').boundingBox()
    expect(island!.y, 'island clears Telegram fullscreen buttons').toBeGreaterThanOrEqual(90)
    for (const [t, name] of [['bank', 'bank'], ['cal', 'calendar'], ['ai', 'assistant'], ['us', 'us'], ['home', 'home-again']] as const) {
      await tab(p, t)
      await audit(p, name + s)
      const nb = await p.locator('.pnav').boundingBox()
      expect.soft(Math.abs(nb!.y - nav!.y), `${name}: tab bar moved`).toBeLessThanOrEqual(1)
      if (t !== 'ai') { const ib = await p.getByTestId('island').boundingBox(); if (ib) expect.soft(Math.abs(ib.y - island!.y), `${name}: island moved`).toBeLessThanOrEqual(1) }
    }
    await tab(p, 'bank')
    await p.locator('.folder').first().click(); await p.locator('.folder').first().click().catch(() => {})
    await audit(p, 'folder' + s)
    await p.goto(`/app/idea/${ids[1]}`); await audit(p, 'idea' + s)
    await p.goto('/app/'); await settle(p)
    await p.getByLabel('Добавить идею').click()
    await expect(p.locator('.sheet')).toBeVisible()
    await audit(p, 'add-sheet' + s)
    await p.keyboard.press('Escape')
    // the round screen: a fresh round on a free day
    const cur = (await api(v, 'GET', '/home')).next
    if (cur) await api(v, 'POST', `/rounds/${cur.id}/cancel`)
    const { round } = await api(v, 'POST', '/rounds')
    await p.goto('/app/round/' + round.id); await audit(p, 'round-time' + s)
    await p.getByLabel('Следующая неделя').click(); await p.waitForTimeout(400)
    await audit(p, 'round-next-week' + s)
  })
}
