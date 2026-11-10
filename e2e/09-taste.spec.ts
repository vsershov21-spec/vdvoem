import { test, expect } from '@playwright/test'
import { people, pair, phone, open, shot, api, addIdeas, tab, BASE } from './lib'

// «Мой вкус»: spheres → kinds → how, saved as you go; the cloud on «Мы», the partner sees it; the ranking leans
// on it at once; the assistant tells the portrait back and «обожаю …» moves it.
test('E40 мой вкус: три уровня, облако, влияет на подборку, помощник «обо мне»', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const ids = await addIdeas(v, ['Джазовый вечер в клубе', 'Стендап в баре', 'Баня на дровах'])
  const ctx = await phone(browser)
  const p = await open(ctx, v)
  await expect(p.getByText('Расскажи о своих вкусах')).toBeVisible()
  await p.getByText('Расскажи о своих вкусах').click()
  await expect(p.getByRole('heading', { name: 'Что тебе нравится?' })).toBeVisible()
  await p.getByRole('button', { name: /^Музыка/ }).click()
  await p.getByRole('button', { name: /^Отдохнуть/ }).click()
  await p.getByRole('button', { name: /^Отдохнуть/ }).click() // twice — «очень»
  await p.getByRole('button', { name: /^Шоу и вечеринки/ }).click()
  await p.getByRole('button', { name: /^Шоу и вечеринки/ }).click()
  await p.getByRole('button', { name: /^Шоу и вечеринки/ }).click() // three times — «не моё»
  await shot(p, 'E40-spheres')
  await p.getByRole('button', { name: 'Дальше' }).click()
  await expect(p.getByRole('heading', { name: 'А точнее?' })).toBeVisible()
  await p.getByRole('button', { name: 'джаз' }).click()
  await p.getByRole('button', { name: 'джаз' }).click()
  await p.getByRole('button', { name: 'баня' }).click()
  await p.getByRole('button', { name: 'Дальше' }).click()
  await expect(p.getByRole('heading', { name: 'Как тебе лучше?' })).toBeVisible()
  await p.getByRole('button', { name: 'Вечера выходных' }).click()
  await p.getByRole('button', { name: /Романтика/ }).click()
  await p.getByRole('button', { name: 'Готово' }).click()
  await expect(p.locator('.tcloud__t', { hasText: 'джаз' })).toBeVisible()
  await expect(p.locator('.tcloud__t', { hasText: 'шоу и вечеринки' })).toBeVisible()
  await shot(p, 'E40-cloud')

  const t = await api(v, 'GET', '/taste')
  expect(t.me.tags['джаз']).toBe(2)
  expect(t.me.spheres['show']).toBe(-1)
  expect(t.me.when).toContain('weekend_evening')

  // the ranking: jazz over stand-up for Вова, before a single mark
  const all = await api(v, 'GET', '/afisha')
  const score = (id: number) => (all.ideas.find((i: any) => i.id === id)?.match?.me ?? 0)
  expect(score(ids[0])).toBeGreaterThan(score(ids[1]))

  // the partner sees it
  const tn = await api(n, 'GET', '/taste')
  expect(tn.partner.cloud.map((x: any) => x.tag)).toContain('джаз')

  // the assistant: the portrait back, and a word in passing moves it
  const r = await api(v, 'POST', '/assistant', { text: 'обо мне' })
  expect(r.reply.text).toMatch(/Очень: .*джаз/)
  await api(n, 'POST', '/assistant', { text: 'обожаю стендап' })
  expect((await api(n, 'GET', '/taste')).me.tags['стендап']).toBe(2)

  // the assistant's button opens the screen (it used to answer «Не знаю такое действие»)
  const q = await open(ctx, v)
  await tab(q, 'ai')
  await q.getByPlaceholder(/Спроси/).fill('обо мне')
  await q.getByLabel('Отправить').click()
  await q.locator('.ai-act', { hasText: /вкус/i }).first().click()
  await expect(q.locator('.tcloud__t', { hasText: 'джаз' })).toBeVisible()
  await expect(q.getByText('Не знаю такое действие')).toHaveCount(0)
  await ctx.close()
  void BASE
})
