import { test, expect } from '@playwright/test'
import { people, say, press, waitMsg, pair, open, phone, shot, api, EMU, lastId } from './lib'

test('E07 идеи из бота: текст, ссылка с разбором, дубль, похожая', async () => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await say(v, 'Театр на Таганке')
  await waitMsg(v, /Сохранил в копилку[\s\S]*Театр на Таганке/)
  await say(v, EMU + '/_emu/place/khachapuri')
  const card = await waitMsg(v, /Хачапури и вино/)
  expect(card.text).toContain('Покровка, 17')
  expect(card.text).toMatch(/Еда/)
  await say(v, 'театр на таганке')
  await waitMsg(v, /уже есть в копилке/)
  const before = await lastId(v)
  await say(v, 'Театр на Таганке вечером')
  await waitMsg(v, /Похоже на «Театр на Таганке»/, 15000, before)
  await press(v, 'Это то же')
  const ideas = (await api(v, 'GET', '/ideas')).ideas
  expect(ideas.filter((i: any) => /Таганке/.test(i.title))).toHaveLength(1)
})

test('E08 копилка и папки: умные папки, поиск, правка, архив', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const [ужин, , выставка] = await (async () => {
    const ids: number[] = []
    for (const t of ['Ужин в грузинском ресторане', 'Прогулка по набережной', 'Выставка в музее', 'Каток в парке', 'Купить продукты']) ids.push((await api(v, 'POST', '/ideas', { text: t })).idea.id)
    return ids
  })()
  await api(v, 'POST', `/ideas/${ужин}/wish`, { wish: 'very' })
  await api(n, 'POST', `/ideas/${ужин}/wish`, { wish: 'want' })
  const ctx = await phone(browser)
  const p = await open(ctx, v, 'ideas')
  // one screen instead of folders: what you both want on top, then your ideas as pictures, categories as chips
  await expect(p.getByTestId('rail-both')).toContainText('Ужин в грузинском ресторане')
  await expect(p.getByText('Ваши идеи · 4')).toBeVisible()
  await expect(p.locator('.grid .icard')).toHaveCount(4)
  await shot(p, 'E08-folders')
  // «Хотим оба» is a filter
  await p.getByRole('tab', { name: /Хотим оба/ }).click()
  await expect(p.locator('.icard')).toHaveCount(1)
  await expect(p.locator('.icard')).toContainText('Ужин в грузинском ресторане')
  await shot(p, 'E08-filter-both')
  await p.getByRole('tab', { name: 'Всё' }).click()
  await p.locator('.fchips').last().getByRole('button', { name: /Еда/ }).click()
  await expect(p.getByText('Ужин в грузинском ресторане')).toBeVisible()
  await p.goto('/app/ideas')
  await p.getByPlaceholder('Найти в копилке').fill('музе')
  await expect(p.locator('.icard')).toHaveCount(1)
  await p.locator('.icard').click()
  await p.getByLabel('Изменить').click()
  await p.locator('.sheet input.input').first().fill('Выставка Айвазовского')
  await p.getByRole('button', { name: 'Сохранить' }).click()
  await expect(p.getByRole('heading', { name: 'Выставка Айвазовского' })).toBeVisible()
  await p.getByRole('button', { name: 'Убрать в архив' }).click()
  await p.goto('/app/ideas/archive')
  await expect(p.getByText('Выставка Айвазовского')).toBeVisible()
  await p.getByText('Выставка Айвазовского').click()
  await p.getByRole('button', { name: 'Вернуть в копилку' }).click()
  expect((await api(v, 'GET', `/ideas/${выставка}`)).idea.status).toBe('active')
  // chores folder: «Сделано»
  await p.goto('/app/ideas/chores')
  await p.getByLabel('Сделано').first().click()
  await expect.poll(async () => (await api(v, 'GET', '/ideas?kind=chore')).ideas.length).toBe(0)
})

test('E09 оценки и приватность: оценку партнёра видно только после своей', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const [id] = (await Promise.all([api(v, 'POST', '/ideas', { text: 'Ужин на крыше' })])).map(r => r.idea.id)
  await api(v, 'POST', `/ideas/${id}/wish`, { wish: 'want' })
  const raw = (await api(n, 'GET', `/ideas/${id}`)).idea
  expect(raw.partner_marked).toBe(true)
  expect(raw.partner_wish).toBeUndefined()
  const ctx = await phone(browser, { dark: true })
  const p = await open(ctx, n, 'idea/' + id)
  await expect(p.getByText(/увидишь после своей отметки/)).toBeVisible()
  await p.getByRole('button', { name: /Очень/ }).click()
  await expect(p.getByText('Вова хочет.')).toBeVisible()
  await expect(p.locator('button[data-w=very][aria-pressed=true] svg')).toBeVisible()
  await p.waitForTimeout(400)
  await shot(p, 'E09-heart-dark')
})
