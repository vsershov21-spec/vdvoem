import { test, expect, type BrowserContext } from '@playwright/test'
import { people, pair, phone, api, addIdeas, shot, waitMsg, press, TOKEN, type U } from './lib'

// Вдвоём 2.0 lives next to the current screens and opens with ?v2=1. These tests follow the accepted
// scenarios (docs/v2/PROJECT.md): the home screen with the swipe deck, «Ждёт тебя», and going back by steps.
async function openV2(ctx: BrowserContext, u: U, path = '') {
  const p = await ctx.newPage()
  p.on('pageerror', e => console.log('pageerror:', e.message))
  const q = new URLSearchParams({ dev: `${u.id}:${u.name}`, devtoken: TOKEN, nointro: '1', v2: '1' })
  await p.goto(`/app/${path}?${q}`)
  await expect(p.getByTestId('v2')).toBeVisible({ timeout: 10000 })
  await expect(p.locator('.splash')).toHaveCount(0, { timeout: 6000 })
  return p
}

test('E50 новая главная: пять вкладок, свайп на весь экран, экран не листается, совпадение', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const ids = await addIdeas(n, ['Планетарий на Баррикадной', 'Каток в Парке Горького'])
  await api(n, 'POST', `/ideas/${ids[0]}/wish`, { wish: 'want' })
  const p = await openV2(await phone(browser), v)
  for (const t of ['home', 'ideas', 'assistant', 'chores', 'plans']) await expect(p.getByTestId('tab-' + t)).toBeVisible()
  await expect(p.getByTestId('tab-assistant')).toContainText('Помощник')
  const card = p.getByTestId('deck-card')
  await expect(card).toBeVisible()
  await expect(p.getByTestId('tile-meet')).toBeVisible()
  await expect(p.getByTestId('tile-chores')).toBeVisible()
  // the home screen does not scroll: everything fits, and the card takes what is left
  const fit = await p.getByTestId('v2-home').evaluate(e => ({ over: e.scrollHeight - e.clientHeight, h: e.clientHeight }))
  expect(fit.over).toBeLessThanOrEqual(0)
  expect((await card.boundingBox())!.height).toBeGreaterThan(fit.h * 0.45)
  await shot(p, 'E50-home')
  // «Не моё» on the first, «Хочу» on the one the partner wants: «Совпало»
  const first = await card.locator('.ttl').innerText()
  if (!first.includes('Планетарий')) { await p.getByTestId('deck-skip').click(); await expect(card.locator('.ttl')).toContainText('Планетарий') }
  await expect(card).toContainText('Настя отметила')
  await p.getByTestId('deck-want').click()
  await expect(p.getByTestId('match-sheet')).toContainText('Совпало')
  await expect(p.getByTestId('match-sheet')).toContainText('Настя тоже хочет')
  await shot(p, 'E50-match')
  await p.getByRole('button', { name: 'Смотреть дальше' }).click()
  await expect(p.getByTestId('match-sheet')).toHaveCount(0)
  expect((await api(v, 'GET', `/ideas/${ids[0]}`)).idea.my_wish).toBe('want')
})

test('E51 «Ждёт тебя»: ответ прямо в строке, «нет» возвращается автору, бот говорит то же', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await api(v, 'POST', '/proposals', { kind: 'time', ref_id: 0, title: 'Планетарий', sub: 'вс 15 ноя, 13:00' })
  await waitMsg(n, /Вова зовёт: Планетарий/)
  const pn = await openV2(await phone(browser), n)
  const row = pn.getByTestId('inbox-row').first()
  await expect(row).toContainText('Вова зовёт: Планетарий')
  await expect(pn.getByTestId('tab-home')).toBeVisible()
  await shot(pn, 'E51-partner')
  await row.getByRole('button', { name: 'Да', exact: true }).click()
  await expect(pn.getByTestId('toast')).toContainText('Ответ отправлен')
  await expect(pn.getByTestId('inbox-row')).toHaveCount(0)
  await waitMsg(v, /Настя ответила «да»/)
  // a chore request declined in the bot: the row comes back to the author with two ways out
  await api(v, 'POST', '/chores', { title: 'Забрать торт', who: 'partner' })
  await waitMsg(n, /Вова просит: Забрать торт/)
  await press(n, 'Не могу')
  const pv = await openV2(await phone(browser), v)
  const back = pv.getByTestId('inbox-row').first()
  await expect(back).toContainText('Настя не может: забрать торт')
  await shot(pv, 'E51-declined')
  await back.getByRole('button', { name: 'Беру сам' }).click()
  await expect(pv.getByTestId('inbox-row')).toHaveCount(0)
  await expect(pv.getByTestId('tile-chores')).toBeVisible()
  const list = (await api(v, 'GET', '/chores')).chores
  expect(list.find((c: any) => c.title === 'Забрать торт')).toMatchObject({ who: 'me', state: 'open' })
})

test('E52 вложенный экран: «‹ Главная» возвращает на шаг назад, вкладка остаётся подсвеченной; тёмная тема', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const p = await openV2(await phone(browser, { dark: true }), v)
  await expect(p.getByTestId('v2')).toHaveClass(/dark/)
  await p.getByLabel('Профиль').click()
  await expect(p.getByRole('button', { name: 'Назад' })).toContainText('Главная')
  await expect(p.getByTestId('tab-home')).toHaveAttribute('aria-current', 'page')
  await shot(p, 'E52-dark')
  await p.getByRole('button', { name: 'Назад' }).click()
  await expect(p.getByTestId('v2-home')).toBeVisible()
  await p.getByTestId('tab-chores').click()
  await expect(p.getByTestId('v2-chores')).toBeVisible()
  await expect(p.getByTestId('tab-chores')).toHaveAttribute('aria-current', 'page')
})
