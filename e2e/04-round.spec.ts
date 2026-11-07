import { test, expect, type Page } from '@playwright/test'
import { people, press, waitMsg, pair, open, phone, shot, api, addIdeas, chat, lastId, planMeeting } from './lib'

const cell = (p: Page, k: string) => p.locator(`.cell[data-k="${k}"]`)
// open the week that holds the day (the grid shows a week at a time)
async function show(p: Page, k: string) {
  if (!(await cell(p, k).count())) await p.getByRole('button', { name: 'Ближайшие дни' }).click()
  for (let i = 0; i < 10 && !(await cell(p, k).count()); i++) await p.getByLabel('Следующая неделя').click()
  await expect(cell(p, k)).toBeVisible()
}
async function mark(p: Page, keys: string[]) { for (const k of keys) { await show(p, k); await cell(p, k).click() } await p.getByRole('button', { name: /Готово/ }).click() }
const days = async (u: any) => (await api(u, 'GET', '/rounds/current')).round.days as string[]

test('E10 выбор: время → колода → совпало → подтверждения → .ics', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(v, ['Ужин в грузинском ресторане', 'Театр на Таганке', 'Прогулка по набережной', 'Каток в парке'])
  const vc = await phone(browser), nc = await phone(browser, { dark: true })
  const vp = await open(vc, v)
  await shot(vp, 'E10-home')
  await vp.getByRole('button', { name: 'Выбрать вместе' }).click()
  await expect(vp.getByText('Когда удобно')).toBeVisible()
  // paint by dragging across one day: morning → evening
  const d = await days(v)
  await show(vp, d[1] + ':0')
  const a = await cell(vp, d[1] + ':0').boundingBox(), b = await cell(vp, d[1] + ':2').boundingBox()
  await vp.mouse.move(a!.x + 20, a!.y + 20); await vp.mouse.down(); await vp.mouse.move(b!.x + 20, b!.y + 20, { steps: 8 }); await vp.mouse.up()
  await expect(vp.locator('.cell[aria-pressed=true]')).toHaveCount(3)
  await shot(vp, 'E10-slots')
  await vp.getByRole('button', { name: /Готово/ }).click()
  await expect(vp.getByText(/Ждём Настю/)).toBeVisible()
  await waitMsg(n, /предлагает куда-нибудь сходить/)
  const np = await open(nc, n)
  await expect(np.getByTestId('round-card')).toContainText('Твой ход')
  await expect(np.getByTestId('island')).toContainText('Твой ход')
  await np.getByTestId('round-card').click()
  await mark(np, [d[1] + ':2'])
  await expect(np.getByText('Выбираем')).toBeVisible()
  await np.waitForTimeout(600)
  await shot(np, 'E10-deck-dark')
  // swipe right on the top card = vote
  const top = np.locator('.dcard').first()
  const fav = (await top.locator('.cover__title').textContent())!
  const box = (await top.boundingBox())!
  await np.mouse.move(box.x + 60, box.y + 120); await np.mouse.down(); await np.mouse.move(box.x + 260, box.y + 130, { steps: 10 }); await np.mouse.up()
  await expect(np.getByText('Твой голос принят')).toBeVisible()
  await expect(vp.getByText('Выбираем')).toBeVisible({ timeout: 12000 })
  await vp.locator('.dcard', { hasText: fav }).getByRole('button', { name: 'Голосую за это' }).click()
  await expect(vp.getByText('Совпало!')).toBeVisible()
  await shot(vp, 'E10-match')
  await vp.getByRole('button', { name: 'Подтверждаю' }).click()
  await expect(vp.getByText(/ждём Настю/)).toBeVisible()
  await waitMsg(n, /подтвердил\(а\)/)
  await press(n, 'Подтвердить')
  await waitMsg(v, /Встреча запланирована/)
  expect((await chat(v)).some(m => m.method === 'sendDocument' && m.file_name === 'vdvoem.ics' && /BEGIN:VEVENT/.test(m.file || ''))).toBeTruthy()
  await vp.reload()
  await expect(vp.getByRole('button', { name: 'В календарь' })).toBeVisible()
  await shot(vp, 'E10-ticket')
  const ics = await vp.request.get(`/api/rounds/${vp.url().match(/round\/(\d+)/)![1]}/ics`, { headers: { 'X-Dev-Token': process.env.DEV_TOKEN!, 'X-Dev-User': String(v.id) } })
  expect(await ics.text()).toContain('SUMMARY:' + fav)
  await vp.goto('/app/')
  await expect(vp.getByTestId('next-card')).toContainText(fav)
})

test('E11 голоса разошлись: компромисс, другая тройка, «Ни один»', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const ids = await addIdeas(v, ['Ужин в ресторане', 'Музей космонавтики', 'Прогулка в парке', 'Каток', 'Кино', 'Боулинг', 'Квиз'])
  for (const id of ids) { await api(v, 'POST', `/ideas/${id}/wish`, { wish: 'want' }); await api(n, 'POST', `/ideas/${id}/wish`, { wish: 'want' }) }
  const { round } = await api(v, 'POST', '/rounds')
  const k = round.days[0] + ':2'
  await api(v, 'POST', `/rounds/${round.id}/slots`, { slots: [k] })
  const r = (await api(n, 'POST', `/rounds/${round.id}/slots`, { slots: [k] })).round
  const first = r.triple.map((i: any) => i.id)
  await api(v, 'POST', `/rounds/${round.id}/vote`, { idea_id: first[0] })
  await api(n, 'POST', `/rounds/${round.id}/vote`, { idea_id: first[1] })
  const np = await open(await phone(browser), n, 'round/' + round.id)
  await expect(np.getByText('Голоса разошлись')).toBeVisible()
  await shot(np, 'E11-mismatch')
  await np.getByRole('button', { name: 'Другая тройка' }).click()
  await expect.poll(async () => (await api(n, 'GET', `/rounds/${round.id}`)).round.triple.map((i: any) => i.id).join()).not.toBe(first.join())
  await np.getByRole('button', { name: 'Ни один' }).click()
  expect((await api(n, 'GET', `/rounds/${round.id}`)).round.my_vote).toBe(-1)
  // compromise: both voted for different ideas both are fine with
  const t = (await api(v, 'GET', `/rounds/${round.id}`)).round.triple.map((i: any) => i.id)
  await api(v, 'POST', `/rounds/${round.id}/vote`, { idea_id: t[0] })
  await api(n, 'POST', `/rounds/${round.id}/vote`, { idea_id: t[1] })
  await np.reload()
  await np.getByRole('button', { name: /вы оба не против/ }).click()
  await expect(np.getByText('Совпало!')).toBeVisible()
})

test('E12 время не совпало: подсказка и повторная отметка', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(v, ['Ужин в ресторане', 'Кино'])
  const { round } = await api(v, 'POST', '/rounds')
  await api(v, 'POST', `/rounds/${round.id}/slots`, { slots: [round.days[1] + ':0'] })
  const np = await open(await phone(browser), n, 'round/' + round.id)
  await mark(np, [round.days[2] + ':2'])
  await expect(np.getByText('Время не совпало')).toBeVisible()
  await waitMsg(v, /Время не совпало/)
  await shot(np, 'E12-no-overlap')
  await np.getByRole('button', { name: 'Изменить моё время' }).click()
  await mark(np, [round.days[1] + ':0'])
  await expect(np.getByText('Выбираем')).toBeVisible()
})

test('E13 другое время сохраняет идею, отмена присылает cancel-.ics', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(v, ['Ужин в ресторане', 'Кино', 'Прогулка'])
  const planned = await planMeeting(v, n)
  const chosen = planned.chosen.title
  const vp = await open(await phone(browser), v, 'round/' + planned.id)
  await vp.getByRole('button', { name: 'Перенести' }).click()
  await expect(vp.getByText('Решили')).toBeVisible()
  await expect(vp.getByText(chosen).first()).toBeVisible()
  await waitMsg(n, /просит другое время/)
  await mark(vp, [planned.days[1] + ':2'])
  const r = (await api(n, 'POST', `/rounds/${planned.id}/slots`, { slots: [planned.days[1] + ':2'] })).round
  expect(r.state).toBe('confirm')
  expect(r.chosen.title).toBe(chosen)
  await api(v, 'POST', `/rounds/${planned.id}/confirm`); await api(n, 'POST', `/rounds/${planned.id}/confirm`)
  await vp.reload()
  const before = await lastId(n)
  await vp.getByRole('button', { name: 'Отменить' }).click()
  await waitMsg(n, /отменил\(а\) выбор/, 10000, before)
  expect((await chat(n)).some(m => m.file_name === 'vdvoem-cancel.ics' && /STATUS:CANCELLED/.test(m.file || ''))).toBeTruthy()
})

