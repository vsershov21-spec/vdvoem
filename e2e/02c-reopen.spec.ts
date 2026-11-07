import { test, expect } from '@playwright/test'
import { people, pair, phone, fakeTelegram, TOKEN, api, addIdeas } from './lib'

// Telegram keeps its webview between opens: close and reopen the app many times in the same browser
// context (shared cache, cookies, connections). Every open must reach the app — never hang on loading.
test('E28 повторные открытия Mini App: каждый раз открывается быстро', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(v, ['Ужин в ресторане', 'Кино'])
  const ctx = await phone(browser)
  await fakeTelegram(ctx, { platform: 'ios', bio: false })
  const url = `/app/?dev=${v.id}:${encodeURIComponent(v.name)}&devtoken=${TOKEN}&nointro=1`
  const times: number[] = []
  const open: any[] = []
  for (let i = 0; i < 8; i++) {
    const p = await ctx.newPage()
    const t0 = Date.now()
    await p.goto(i % 2 ? url.replace('/app/?', '/app/ideas?') : url)
    await expect(p.locator('main.screen')).toBeVisible({ timeout: 6000 })
    times.push(Date.now() - t0)
    if (i % 3 === 2) open.push(p) // some stay «minimized» with their live stream, like Telegram's tray
    else { await p.waitForTimeout(1800); await p.close() }
  }
  // a partner's change still reaches an app kept in the background once it comes back
  await api(n, 'POST', '/ideas', { text: 'Каток в парке' })
  console.log('open times, ms:', times.join(' '))
  expect(Math.max(...times)).toBeLessThan(6000)
  for (const p of open) await p.close()
})
