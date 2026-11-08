import { test, expect } from '@playwright/test'
import { people, pair, phone, open, shot, api, waitMsg, tab, forward, say, lastId, BASE } from './lib'

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

test('E32 пост из канала: пересылаешь боту — событие с датой и ценой в «Афише»; выгрузка канала целиком', async () => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await forward(v, 'Билеты дёшево', '🎭 «Вишнёвый сад» в Современнике — 15 декабря, 19:00. Билеты от 900 ₽, пишите в личку')
  await waitMsg(v, /Читаю пост из «Билеты дёшево»/)
  const m = await waitMsg(v, /в афишу/, 30000)
  expect(m.text).toContain('Вишнёвый сад')
  expect(m.text).toMatch(/1\s800/) // 900 a ticket → for two
  const af = (await api(v, 'GET', '/afisha')).ideas
  const sad = af.find((i: any) => /Вишн/.test(i.title))
  expect(sad.why).toContain('Билеты дёшево')
  expect(new Date(sad.starts_at * 1000).getUTCHours()).toBe(16) // 19:00 Moscow
  // a whole channel export, from the server itself
  const space = (await api(v, 'GET', '/me')).space.id
  const posts = ['🔴ЧАЙКА\n🏛МХТ имени А.П. Чехова\n✅ 19 декабря 19.00\n🔥4500 вместо 5500', 'Всем привет, скоро новые спектакли', '🔴 МЕРА ЗА МЕРУ\n🗓 20 декабря 19.00\n🔥3200 (вместо 4800)']
  const r = await (await fetch(BASE + '/agent/channel', { method: 'POST', body: JSON.stringify({ space, from: 'Билеты дёшево', posts }) })).json()
  expect(r).toMatchObject({ added: 2, posts: 3 })
  await waitMsg(n, /Из канала «Билеты дёшево»/)
})

test('E33 чат: подсказки при печати, следующий шаг в одно касание, бот отвечает на просьбу текстом', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const space = (await api(v, 'GET', '/me')).space.id
  const picks = [{ title: 'Щелкунчик в Кремлёвском дворце', category: 'culture', starts: day(4, '19:00'), price_rub: 6000, why: 'классика', score: 9, url: 'https://afisha.example/e33/' + space + '/nut', image: '' }]
  await (await fetch(BASE + '/agent/picks', { method: 'POST', body: JSON.stringify({ space, picks }) })).json()
  const p = await open(await phone(browser), v)
  await tab(p, 'ai')
  // completion: a phrase, then a title after «сколько стоит»
  const field = p.getByPlaceholder(/Спроси/)
  await field.fill('что на вы')
  await expect(p.getByTestId('type-hints')).toContainText('Что есть на выходных?')
  await field.fill('сколько стоит щелк')
  await expect(p.getByTestId('type-hints')).toContainText('Сколько стоит «Щелкунчик в Кремлёвском дворце»?')
  await shot(p, 'E33-hints')
  await p.getByTestId('type-hints').getByRole('button').first().click()
  await expect(p.locator('.msg--ai').last()).toContainText('₽', { timeout: 10000 })
  // the answer brings its own next steps, and each is understood at once
  if (!(await p.getByTestId('follow').count())) console.log('E33 debug', await p.evaluate(() => JSON.stringify({ docks: [...document.querySelectorAll('.sugg')].map(e => e.outerHTML.slice(0, 200)), field: (document.querySelector('.composer textarea') as HTMLTextAreaElement)?.value, cls: document.body.className, last: document.querySelector('.chat')?.lastElementChild?.previousElementSibling?.outerHTML.slice(0, 400) })),
    JSON.stringify((await api(v, 'GET', '/assistant')).messages.slice(-1)[0]?.reply).slice(0, 600))
  await expect(p.getByTestId('follow')).toBeVisible()
  await p.getByTestId('follow').getByRole('button', { name: 'Расскажи про это' }).click()
  await expect(p.locator('.msg--ai').last()).toContainText('Щелкунчик', { timeout: 10000 })
  await shot(p, 'E33-next')
  // in the bot a request is answered, a bare name is still an idea
  const before = await lastId(v)
  await say(v, 'хотим что-нибудь на выходных')
  await waitMsg(v, /Лучше всего|Щелкунчик|ничего/i, 15000, before)
  expect((await api(v, 'GET', '/ideas')).ideas.some((i: any) => /хотим что-нибудь/i.test(i.title))).toBe(false)
})
