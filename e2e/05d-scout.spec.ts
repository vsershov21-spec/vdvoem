import { test, expect } from '@playwright/test'
import { people, pair, phone, open, shot, api, addIdeas, waitMsg, press, tab, BASE } from './lib'

const day = (n: number, hm = '19:00') => { const d = new Date(Date.now() + n * 86400_000); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}T${hm}` }

test('E30 скаут: агент изнутри сервера, драфт недели и анонсы, вкусы каждого, помощник роется в драфте', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const ids = await addIdeas(v, ['Спектакль в театре Вахтангова', 'Стендап вечер', 'Хинкальная на Арбате'])
  await api(v, 'POST', `/ideas/${ids[0]}/wish`, { wish: 'very' })
  await api(v, 'POST', `/ideas/${ids[1]}/wish`, { wish: 'skip' })
  await api(n, 'POST', `/ideas/${ids[2]}/wish`, { wish: 'very' })
  const space = (await api(v, 'GET', '/me')).space.id
  const goal = (await api(n, 'POST', '/goals', { text: 'театр в январе' })).id

  // the agent's door is only on the server itself: through the proxy it does not exist
  expect((await fetch(BASE + '/agent/briefs', { headers: { 'X-Forwarded-For': '1.2.3.4' } })).status).toBe(404)
  const briefs = (await (await fetch(BASE + '/agent/briefs')).json()).briefs
  const b = briefs.find((x: any) => x.space === space)
  expect(b.city).toBe('Москва')
  expect(b.people).toHaveLength(2)
  expect(b.goals[0].text).toBe('театр в январе')
  expect(JSON.stringify(b)).not.toContain('Вова') // no names leave the app

  const picks = [
    ['Премьера в театре Маяковского', 'culture'], ['Фестиваль грузинской кухни', 'food'], ['Большой стендап-концерт', 'other'],
    ['Прогулка по ВДНХ', 'walk'], ['Выставка в Пушкинском', 'culture'], ['Каток на ВДНХ', 'sport'], ['Квиз в баре', 'other'],
  ].map(([title, category], k) => ({ title, category, starts: day(3, k % 2 ? '19:00' : '14:00'), price_rub: 1500 + k * 400, why: 'похоже на то, что вы любите', score: 6, url: 'https://afisha.example/e2e/' + space + '/' + k, image: '' }))
  picks.push({ title: 'Гамлет в театре Наций', category: 'culture', starts: day(60), price_rub: 6000, why: 'премьера сезона', score: 8, url: 'https://afisha.example/e2e/' + space + '/hamlet', image: '', horizon: 'later', on_sale: day(5, '10:00'), goal_id: goal } as any)
  const r = await (await fetch(BASE + '/agent/picks', { method: 'POST', body: JSON.stringify({ space, picks }) })).json()
  expect(r).toMatchObject({ added: 8, to_bank_and_bot: 5 })

  // the digest reaches both, with marks right in the bot
  const dg = await waitMsg(n, /Скаут нашёл для вас/)
  expect(dg.text).toContain('Анонсы на потом')
  expect(dg.text).toContain('Гамлет')
  await press(n, '1 · Хочу')

  // «Афиша»: the week and announcements; marking a draft puts it in the bank
  const p = await open(await phone(browser), v, 'ideas/scout')
  await p.getByRole('tab', { name: /Ближайшие 2 недели/ }).click()
  await p.getByRole('button', { name: 'Фильтры' }).click() // the rest of the filters live in a sheet: one row on screen
  await p.locator('.sheet').getByRole('button', { name: /Скаут/ }).click()
  await p.locator('.sheet').getByRole('button', { name: /Показать/ }).click()
  await expect(p.locator('.acard')).toHaveCount(7)
  await expect(p.locator('.acard').first()).toContainText('%')
  await shot(p, 'E30-afisha')
  const draft = p.locator('.acard', { hasText: 'Квиз в баре' })
  await draft.getByRole('button', { name: 'Очень' }).click()
  await expect.poll(async () => (await api(v, 'GET', '/ideas')).ideas.some((i: any) => i.title === 'Квиз в баре')).toBe(true)
  await p.getByRole('tab', { name: /Анонсы/ }).click()
  await expect(p.locator('.acard')).toHaveCount(1)
  await expect(p.locator('.acard')).toContainText('Продажа билетов')
  await shot(p, 'E30-later')

  // the assistant digs through the draft (rules, no model here)
  await p.goto('/app/assistant')
  await p.getByRole('textbox').fill('что из афиши в эту неделю до 2500 без стендапа?')
  await p.getByRole('textbox').press('Enter')
  await expect(p.locator('.ai-card').first()).toBeVisible({ timeout: 15000 })
  await expect(p.locator('.ai-cards')).not.toContainText('Большой стендап-концерт')
  await p.getByRole('textbox').fill('Запомни, что я не люблю стендап')
  await p.getByRole('textbox').press('Enter')
  await expect(p.locator('.chat')).toContainText('Запомнил')
  await shot(p, 'E30-assistant')

  // «Мы»: my taste and my memory; Настя does not see mine
  await tab(p, 'us')
  await expect(p.getByTestId('taste')).toContainText('я не люблю стендап')
  await expect(p.getByTestId('goals')).toContainText('театр в январе')
  await shot(p, 'E30-us')
  expect((await api(n, 'GET', '/profile')).memories).toHaveLength(0)
})
