import { test, expect } from '@playwright/test'
import { people, pair, phone, open, shot, api, waitMsg, tab, forward, BASE } from './lib'

const day = (n: number, hm = '19:00') => { const d = new Date(Date.now() + n * 86400_000); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}T${hm}` }

test('E31 помощник v2: цена из данных, настроение, «запиши нас» — сразу в календарь без выбора времени', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const space = (await api(v, 'GET', '/me')).space.id
  const picks = [
    { title: 'Джазовый концерт в Козловском клубе', category: 'culture', starts: day(3, '19:00'), price_rub: 3000, why: 'живой звук', score: 9, url: 'https://afisha.example/e31/' + space + '/jazz', image: '' },
    { title: 'СПА-вечер в банях', category: 'other', starts: day(3, '00:00').slice(0, 10), price_rub: 7000, why: 'отдохнуть', score: 7, url: 'https://afisha.example/e31/' + space + '/spa', image: '' },
  ]
  await (await fetch(BASE + '/agent/picks', { method: 'POST', body: JSON.stringify({ space, picks }) })).json()

  const p = await open(await phone(browser), v)
  await tab(p, 'ai')
  const ask = async (q: string) => {
    const before = await p.locator('.msg--ai').count()
    const t0 = Date.now()
    await p.getByPlaceholder(/Спроси/).fill(q)
    await p.getByLabel('Отправить').click()
    await expect(p.locator('.msg--ai')).toHaveCount(before + 1, { timeout: 10000 })
    expect(Date.now() - t0).toBeLessThan(7000) // the «≤ 6–7 s» promise, here with rules only
    return p.locator('.msg--ai').last()
  }
  await expect(await ask('сколько стоит джазовый концерт?')).toContainText('₽')
  await expect(await ask('хотим почилить')).toContainText(/настроение/i)
  await expect(p.locator('.ai-card').last()).toBeVisible()
  await expect(p.getByTestId('follow')).toBeVisible() // refine in one tap
  await shot(p, 'E31-chill')
  await expect(await ask('запиши нас на джазовый концерт')).toContainText('Позвал')
  await shot(p, 'E31-book')

  // Настя gets «зовёт» with one button; after it both have a planned meeting at 19:00
  await waitMsg(n, /зовёт/)
  const rd = (await api(n, 'GET', '/rounds/current')).round
  expect(rd.state).toBe('confirm')
  await api(n, 'POST', `/rounds/${rd.id}/confirm`)
  await expect.poll(async () => (await api(v, 'GET', `/rounds/${rd.id}`)).round.state).toBe('planned')

  // an event with its own time says so on its card
  const jazz = (await api(v, 'GET', '/ideas')).ideas.find((i: any) => /Джазовый/.test(i.title))
  const spa = (await api(v, 'GET', '/ideas?status=draft')).ideas?.find?.((i: any) => /СПА/.test(i.title))
  await p.goto('/app/idea/' + jazz.id)
  await expect(p.getByTestId('fixed-when')).toBeVisible()
  if (spa) {
    await p.goto('/app/idea/' + spa.id)
    await expect(p.getByTestId('fixed-when')).toHaveCount(0)
  }
})

test('E32 пост из канала: пересылаешь боту — он принимает, без модели кладёт в копилку', async () => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await forward(v, 'Билеты дёшево', '🎭 «Вишнёвый сад» в Современнике — 15 декабря, 19:00. Билеты от 900 ₽, пишите в личку')
  await waitMsg(v, /Читаю пост из «Билеты дёшево»/)
  await waitMsg(v, /сохранил его в копилку/, 30000)
  await expect.poll(async () => (await api(v, 'GET', '/ideas')).ideas.some((i: any) => /Вишн/.test(i.title)), { timeout: 20000 }).toBe(true)
})
