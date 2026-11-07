import { test, expect } from '@playwright/test'
import { people, pair, open, phone, shot, api, addIdeas } from './lib'

const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

test('E24 время на любой день: недели, календарь, общий вечер через три недели', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(v, ['Ужин в ресторане', 'Кино', 'Театр'])
  const p = await open(await phone(browser), v)
  await p.getByRole('button', { name: 'Выбрать вместе' }).click()
  await expect(p.getByText('Когда удобно')).toBeVisible()
  await expect(p.locator('.slots__day')).toHaveCount(7)
  await shot(p, 'E24-week')
  // weeks: forward, back; the first week cannot go back
  const title = await p.locator('.wknav__mid').textContent()
  await p.getByLabel('Следующая неделя').click()
  await expect(p.locator('.wknav__mid')).not.toHaveText(title!)
  await p.getByRole('button', { name: 'Эта неделя' }).click()
  await expect(p.getByLabel('Предыдущая неделя')).toBeDisabled()
  // past parts of today are locked
  const today = ymd(new Date())
  if (new Date().getHours() >= 13) await expect(p.locator(`.cell[data-k="${today}:0"]`)).toBeDisabled()
  // a date three weeks ahead through the month picker
  const far = new Date(); far.setDate(far.getDate() + 21)
  await p.getByRole('button', { name: 'Другая дата' }).click()
  await expect(p.getByText('Выбери день')).toBeVisible()
  if (far.getMonth() !== new Date().getMonth()) await p.locator('.sheet').getByLabel('Следующий месяц').click()
  await shot(p, 'E24-picker')
  await p.locator('.sheet').getByLabel(ymd(far)).click()
  const k = ymd(far) + ':2'
  await expect(p.locator(`.cell[data-k="${k}"]`)).toBeVisible()
  await p.locator(`.cell[data-k="${k}"]`).click()
  // the mark is remembered while moving between weeks, and a chip leads back to it
  await p.getByRole('button', { name: 'Эта неделя' }).click()
  await expect(p.locator('.wkchip', { hasText: /\d/ }).filter({ has: p.locator('b') })).toHaveCount(1)
  await p.locator('.wkchip').filter({ has: p.locator('b') }).click()
  await expect(p.locator(`.cell[data-k="${k}"]`)).toHaveAttribute('aria-pressed', 'true')
  await shot(p, 'E24-far-week')
  await p.getByRole('button', { name: /Готово · 1 слот/ }).click()
  await expect(p.getByText(/Ждём Настя/)).toBeVisible()
  const { round } = await api(n, 'GET', '/rounds/current')
  const r = (await api(n, 'POST', `/rounds/${round.id}/slots`, { slots: [k] })).round
  expect(r.state).toBe('vote')
  expect(r.windows[0].from * 1000).toBeGreaterThan(far.getTime() - 86400_000)
  await expect(p.getByText('Выбираем')).toBeVisible({ timeout: 12000 })
})
