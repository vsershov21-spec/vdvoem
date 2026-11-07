import { test, expect } from '@playwright/test'
import { people, pair, phone, api, press, waitMsg, shot, fakeTelegram, TOKEN } from './lib'

// the Face ID lock lives in Telegram's BiometricManager; desktop clients have none
test('E25 защита Face ID: компьютер не блокируется, на телефоне — Face ID или подтверждение в боте', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await api(v, 'PATCH', '/security/prefs', { biometric_lock: true })
  const url = `/app/?dev=${v.id}:${encodeURIComponent(v.name)}&devtoken=${TOKEN}&nointro=1`

  // Telegram Desktop: no sensor → opens straight away
  const desk = await browser.newContext({ baseURL: process.env.BASE || 'http://localhost:18099', viewport: { width: 420, height: 760 }, locale: 'ru-RU' })
  await fakeTelegram(desk, { platform: 'tdesktop', bio: false })
  const dp = await desk.newPage()
  await dp.goto(url)
  await expect(dp.locator('.pnav')).toBeVisible({ timeout: 10000 })
  await expect(dp.getByText('С возвращением')).toHaveCount(0)

  // iPhone, Face ID works
  const ok = await phone(browser)
  await fakeTelegram(ok, { platform: 'ios', bio: true, authOk: true })
  const op = await ok.newPage()
  await op.goto(url)
  await expect(op.locator('.pnav')).toBeVisible({ timeout: 10000 })
  expect(await op.evaluate(() => (window as any).__bio)).toContain('auth')
  expect(await op.evaluate(() => (window as any).__fullscreen)).toBe(true) // phones open fullscreen

  // iPhone, Face ID fails → the bot confirms
  const bad = await phone(browser)
  await fakeTelegram(bad, { platform: 'ios', bio: true, authOk: false })
  const bp = await bad.newPage()
  await bp.goto(url)
  await expect(bp.getByText('С возвращением')).toBeVisible({ timeout: 10000 })
  await expect(bp.getByRole('alert')).toContainText('Не получилось')
  await shot(bp, 'E25-locked')
  await bp.getByRole('button', { name: 'Подтвердить в боте' }).click()
  await waitMsg(v, /Открыть «Вдвоём» без Face ID\?/)
  await press(v, 'Да, это я')
  await expect(bp.locator('.pnav')).toBeVisible({ timeout: 10000 })

  // the toggle can be switched off from the desktop
  await dp.goto(url.replace('/app/?', '/app/us?'))
  await dp.getByText('Защита Face ID').click()
  await expect.poll(async () => (await api(v, 'GET', '/security')).biometric_lock).toBe(false)
})
