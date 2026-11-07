import { test, expect, type Page, type BrowserContext } from '@playwright/test'

const EMU = process.env.EMU!
const TOKEN = process.env.DEV_TOKEN!
const VOVA = { id: 101, name: 'Вова' }, NASTYA = { id: 202, name: 'Настя' }
const shot = (p: Page, n: string) => p.screenshot({ path: `shots/${n}.png` })

async function say(u: { id: number; name: string }, text: string) {
  const r = await fetch(EMU + '/_emu/say', { method: 'POST', body: JSON.stringify({ from: u.id, name: u.name, text }) })
  expect(r.ok, await r.text()).toBeTruthy()
}
async function press(u: { id: number; name: string }, label: string) {
  const r = await fetch(EMU + '/_emu/press', { method: 'POST', body: JSON.stringify({ from: u.id, name: u.name, label }) })
  expect(r.ok, await r.text()).toBeTruthy()
}
async function chat(u: { id: number }): Promise<{ text: string; method: string; file_name?: string; markup?: any }[]> {
  return (await fetch(EMU + '/_emu/chat/' + u.id)).json()
}
async function waitFor(u: { id: number }, re: RegExp, timeout = 15000) {
  const t0 = Date.now()
  while (Date.now() - t0 < timeout) {
    const m = (await chat(u)).find(x => re.test(x.text))
    if (m) return m
    await new Promise(r => setTimeout(r, 300))
  }
  throw new Error(`no message ${re} for ${u.id}: ` + JSON.stringify((await chat(u)).map(x => x.text).slice(-5)))
}
async function openApp(ctx: BrowserContext, u: { id: number; name: string }, path = '') {
  const p = await ctx.newPage()
  await p.goto(`/app/${path}?dev=${u.id}:${encodeURIComponent(u.name)}&devtoken=${TOKEN}`)
  return p
}

test('пара: знакомство, копилка, выбор, встреча, помощник', async ({ browser }) => {
  // --- bot: invite & pairing ---
  await say(VOVA, '/start')
  const inv = await waitFor(VOVA, /Пригласи партнёра/)
  const code = inv.text.match(/start=(inv_[A-Z0-9]+)/)![1]
  await say(NASTYA, '/start ' + code)
  await waitFor(VOVA, /Настя с тобой/)
  await waitFor(NASTYA, /Вы с Вова теперь вдвоём/)
  await say(NASTYA, '/start ' + code)
  await waitFor(NASTYA, /Привет, Настя/)

  // --- bot: ideas from chat; partner marks one ---
  await say(VOVA, 'Ужин в грузинском ресторане')
  await say(VOVA, 'Театр на Таганке')
  await say(NASTYA, 'Прогулка по набережной')
  await say(NASTYA, 'Каток в парке')
  await waitFor(VOVA, /Театр на Таганке/)
  await press(NASTYA, 'Очень хочу')
  await say(VOVA, 'ужин в грузинском ресторане')
  await waitFor(VOVA, /уже есть в копилке/)
  await say(VOVA, 'Куда сходить в субботу?')
  await waitFor(VOVA, /Подобрал|фаворит|нет подходящих|ничего нет/i)

  // --- app: two phones ---
  const vctx = await browser.newContext(), nctx = await browser.newContext({ colorScheme: 'dark' })
  const v = await openApp(vctx, VOVA)
  await expect(v.getByText('Куда сходим на выходных?')).toBeVisible()
  await v.waitForTimeout(900)
  await shot(v, '01-home')
  // mark ideas in the rail
  const rail = v.locator('.rail .icard')
  await expect(rail.first()).toBeVisible()
  await rail.first().getByRole('button', { name: 'Очень' }).click()
  await v.waitForTimeout(700)
  await rail.first().getByRole('button', { name: 'Хочу' }).click()
  await v.waitForTimeout(700)
  // add via sheet → flies into копилка
  await v.getByLabel('Добавить идею').click()
  await v.getByPlaceholder(/Ссылка на место/).fill('Выставка в музее')
  await shot(v, '02-add-sheet')
  await v.getByRole('button', { name: 'В копилку' }).click()
  await v.waitForTimeout(1100)
  await v.locator('#tab-jar').click()
  await expect(v.getByText('Выставка в музее')).toBeVisible()
  await v.waitForTimeout(600)
  await shot(v, '03-kopilka')
  // idea details
  await v.getByText('Театр на Таганке').click()
  await expect(v.getByText('Твоя оценка')).toBeVisible()
  await v.getByRole('button', { name: 'Хочу' }).click()
  await shot(v, '04-idea')

  // Nastya marks too (dark theme phone)
  const n = await openApp(nctx, NASTYA, 'ideas')
  await expect(n.getByText('Театр на Таганке')).toBeVisible()
  await n.getByText('Театр на Таганке').click()
  await n.getByRole('button', { name: 'Очень' }).click()
  await n.waitForTimeout(500)
  await shot(n, '05-idea-dark-partner-visible')

  // --- round: start, slots ---
  await v.locator('#tab-home').click()
  await v.getByRole('button', { name: 'Выбрать вместе' }).click()
  await expect(v.getByText('Когда удобно')).toBeVisible()
  const cells = v.locator('.cell')
  await cells.nth(2).click(); await cells.nth(4).click(); await cells.nth(5).click(); await cells.nth(8).click()
  await shot(v, '06-slots')
  await v.getByRole('button', { name: /Готово/ }).click()
  await expect(v.getByText(/Ждём Настя/)).toBeVisible()
  await v.waitForTimeout(500)
  await shot(v, '07-waiting')

  await waitFor(NASTYA, /предлагает куда-нибудь сходить/)
  await n.goto(`/app/?dev=${NASTYA.id}:${encodeURIComponent(NASTYA.name)}&devtoken=${TOKEN}`)
  await expect(n.getByText(/Твой ход/)).toBeVisible()
  await shot(n, '08-home-round-dark')
  await n.locator('.frame').first().click()
  const nc = n.locator('.cell')
  await nc.nth(5).click(); await nc.nth(8).click()
  await n.getByRole('button', { name: /Готово/ }).click()

  // --- vote ---
  await expect(n.getByText('Выбираем')).toBeVisible()
  await n.waitForTimeout(700)
  await shot(n, '09-deck-dark')
  const favTitle = (await n.locator('.dcard').first().locator('.cover__title').textContent())!
  await n.locator('.dcard').first().getByRole('button', { name: 'Голосую за это' }).click()
  await expect(n.getByText('Твой голос принят')).toBeVisible()

  await v.waitForTimeout(5600) // polling picks up the new state
  await expect(v.getByText('Выбираем')).toBeVisible({ timeout: 10000 })
  await shot(v, '10-deck')
  const vcard = v.locator('.dcard', { hasText: favTitle })
  await v.locator('.deckdots i').nth(0).click()
  await vcard.getByRole('button', { name: /Голосую за это/ }).click()
  await expect(v.getByText('Совпало!')).toBeVisible()
  await v.waitForTimeout(250)
  await shot(v, '11-match-burst')
  await v.waitForTimeout(1300)
  await shot(v, '12-confirm')
  await v.getByRole('button', { name: 'Подтверждаю' }).click()
  await expect(v.getByText(/ждём Настя/)).toBeVisible()

  // Nastya confirms from the bot button
  await waitFor(NASTYA, /подтвердил\(а\)/)
  await press(NASTYA, 'Подтвердить')
  await waitFor(VOVA, /Встреча запланирована/)
  const docs = (await chat(VOVA)).filter(m => m.method === 'sendDocument')
  expect(docs.length).toBeGreaterThan(0)
  await v.reload()
  await expect(v.getByRole('button', { name: 'В календарь' })).toBeVisible()
  await shot(v, '13-planned-ticket')

  // --- assistant ---
  await v.locator('#tab-spark').click()
  await expect(v.getByText('Чем помочь?')).toBeVisible()
  await shot(v, '14-assistant-empty')
  await v.getByRole('button', { name: 'Что у нас запланировано?' }).click()
  await expect(v.locator('.msg--ai').last()).toContainText(/Ближайшая встреча|Ничего/)
  await v.getByPlaceholder(/Спроси/).fill('добавь: каток в Парке Горького')
  await v.getByLabel('Отправить').click()
  await expect(v.locator('.ai-act').last()).toBeVisible()
  await v.waitForTimeout(500)
  await shot(v, '15-assistant')
  await v.locator('.ai-act').last().click()
  await expect(v.locator('.ai-act[data-done]')).toBeVisible()

  // --- us & settings ---
  await v.locator('#tab-us').click()
  await expect(v.getByText('Настройки')).toBeVisible()
  await v.getByText('Бот сам предлагает идеи').click()
  await v.getByText('Рандом', { exact: true }).click()
  await v.waitForTimeout(500)
  await shot(v, '16-suggest-settings')

  // --- bot surprise ---
  await say(VOVA, '/surprise')
  await waitFor(VOVA, /Случайная идея|нечего предложить|Больше подходящих/)
  await shot(n, '17-dark-final')
})
