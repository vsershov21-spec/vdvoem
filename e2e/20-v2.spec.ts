import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { newPeople as people, people as oldPeople, open, pair, phone, api, addIdeas, shot, waitMsg, noMsg, press, say, chat, advance, lastId, TOKEN, BASE, type U } from './lib'

// Вдвоём 2.0 is the app: it opens with no parameter, and these people say nothing about their interface (the
// old screens are behind ?v2=0, files 01–19; E72 is the switch itself). These tests follow the accepted
// scenarios (docs/v2/PROJECT.md): the home screen with the swipe deck, «Ждёт тебя», and going back by steps.
async function openV2(ctx: BrowserContext, u: U, path = '') {
  const p = await ctx.newPage()
  p.on('pageerror', e => console.log('pageerror:', e.message))
  const q = new URLSearchParams({ dev: `${u.id}:${u.name}`, devtoken: TOKEN, nointro: '1' })
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
  await waitMsg(n, /Вова зовёт/)
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
  await waitMsg(n, /Вова просит/)
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

test('E53 идеи: новая по названию, автор дописывает цену и программу, видно кто заполнил; партнёр отмечает — «Совпало» и «Идём»', async ({ browser }) => {
  test.setTimeout(120_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const p = await openV2(await phone(browser), v, 'ideas')
  await p.getByTestId('add-idea').click()
  await p.getByTestId('add-sheet').locator('input').fill('Каток на ВДНХ')
  await p.getByTestId('add-save').click()
  await expect(p.getByTestId('v2-idea')).toContainText('Каток на ВДНХ')
  await expect(p.getByRole('button', { name: 'Назад' })).toContainText('Идеи')
  await expect(p.getByTestId('v2-idea')).toContainText('Не хватает')
  // the author fills in the price and the programme: the sheet opens with the current values
  await p.getByTestId('edit-idea').click()
  const sheet = p.getByTestId('edit-sheet')
  await expect(sheet.locator('input').first()).toHaveValue('Каток на ВДНХ')
  await sheet.getByPlaceholder('например, 3000').fill('1500')
  await sheet.getByPlaceholder('что входит, во сколько сбор').fill('Прокат на месте, 1,5 часа')
  await p.getByTestId('save-idea').click()
  await expect(p.getByTestId('toast')).toContainText('Сохранено')
  const fields = p.getByTestId('fields')
  await expect(fields).toContainText('1 500')
  await expect(fields).toContainText('Прокат на месте')
  await expect(fields.locator('.by').first()).toHaveText('я')
  await shot(p, 'E53-card')
  await p.getByRole('button', { name: '♥ Хочу' }).click()
  // the partner: the idea is in «Наши» as Вова's, the field is marked with his name; «Хочу» makes a match
  const id = (await api(v, 'GET', '/ideas')).ideas.find((i: any) => i.title === 'Каток на ВДНХ').id
  const q = await openV2(await phone(browser), n, 'ideas')
  await expect(q.getByTestId('idea-card').first()).toContainText('от Вовы')
  await q.getByTestId('idea-card').first().click()
  await expect(q.getByTestId('fields').locator('.by').first()).toHaveText('Вова')
  await expect(q.getByTestId('edit-idea')).toHaveCount(0)
  await q.getByRole('button', { name: '♥ Хочу' }).click()
  await expect(q.getByTestId('toast')).toContainText('Совпало')
  await q.getByRole('button', { name: 'Назад' }).click()
  await q.getByRole('tab', { name: /Совпало · 1/ }).click()
  await shot(q, 'E53-both')
  await q.getByTestId('match-row').getByRole('button', { name: 'Идём' }).click()
  await expect(q).toHaveURL(/\/app\/meet\/\d+/)
  expect((await api(n, 'GET', '/rounds/current')).round.chosen.id).toBe(id)
})

test('E54 афиша: список, фильтр и дни; у события поля от скаута и «Позвать»', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const space = (await api(v, 'GET', '/me')).space.id
  const today = new Date((await api(v, 'GET', '/calendar')).today + 'T12:00')
  const at = (k: number) => { const d = new Date(today.getTime() + k * 86400_000); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}T19:00` }
  const picks = [['Органный концерт при свечах', 'culture'], ['Ужин в грузинском ресторане', 'food']].map(([title, category], k) =>
    ({ title, category, starts: at(k + 1), price_rub: 3000, why: 'под ваши вкусы', score: 7, url: `https://afisha.example/e54/${space}/${k}`, image: '', address: 'Малая Грузинская, 27' }))
  await fetch(BASE + '/agent/picks', { method: 'POST', body: JSON.stringify({ space, picks }) })
  const p = await openV2(await phone(browser), v, 'ideas')
  await p.getByRole('tab', { name: 'Афиша' }).click()
  await expect(p.getByTestId('afisha-row')).toHaveCount(2)
  await p.getByRole('button', { name: 'Еда' }).click()
  await expect(p.getByTestId('afisha-row')).toHaveCount(1)
  await p.getByRole('button', { name: 'Всё' }).click()
  await p.getByRole('button', { name: 'По дням' }).click()
  await expect(p.locator('.cal .d').first()).toBeVisible()
  await shot(p, 'E54-days')
  await p.getByRole('button', { name: 'Списком' }).click()
  await p.getByTestId('afisha-row').filter({ hasText: 'Органный' }).click()
  await expect(p.getByTestId('fields').locator('.by').first()).toHaveText('скаут')
  await expect(p.getByTestId('go-meet')).toContainText('Позвать Настю')
  await shot(p, 'E54-event')
})

test('E55 встреча: время предлагает один, «Да» второго ставит в планы; место «а потом» с отказом; отмена видна партнёру', async ({ browser }) => {
  test.setTimeout(150_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const [id] = await addIdeas(n, ['Планетарий на Баррикадной'])
  await api(n, 'POST', `/ideas/${id}/wish`, { wish: 'want' })
  await api(v, 'POST', `/ideas/${id}/wish`, { wish: 'want' })
  const p = await openV2(await phone(browser), v, 'idea/' + id)
  await p.getByTestId('go-meet').click()
  await expect(p.getByTestId('meet-status')).toHaveText('без времени')
  // the offer: a slot of the grid, tomorrow in the afternoon
  await p.getByTestId('offer-time').click()
  await p.getByTestId('time-sheet').locator('.slot').nth(4).click()
  await expect(p.getByTestId('offer-send')).toContainText('Предложить Насте')
  await shot(p, 'E55-time')
  await p.getByTestId('offer-send').click()
  await expect(p.getByTestId('meet-status')).toHaveText('ждём Настю')
  const rid = Number(p.url().match(/meet\/(\d+)/)![1])
  expect((await api(v, 'GET', `/rounds/${rid}`)).round.state).not.toBe('planned')
  // the partner answers on the home screen: only now is it planned
  const q = await openV2(await phone(browser), n)
  const row = q.getByTestId('inbox-row').first()
  await expect(row).toContainText('Вова зовёт: Планетарий')
  await row.getByRole('button', { name: 'Да', exact: true }).click()
  await expect(q.getByTestId('inbox-row')).toHaveCount(0)
  await expect(p.getByTestId('meet-status')).toHaveText('в планах', { timeout: 30000 })
  expect((await api(v, 'GET', `/rounds/${rid}`)).round.state).toBe('planned')
  await shot(p, 'E55-planned')
  // tickets: who buys is seen by the other
  await p.getByTestId('buy-me').click()
  await expect(p.getByTestId('meet-tickets')).toContainText('Покупаешь ты')
  // a place after: offered, declined, the «нет» stays in the card of the one who offered
  await p.getByTestId('add-stop').click()
  await p.getByTestId('stop-sheet').locator('input').fill('Кафе у Патриарших')
  await p.getByTestId('stop-own').click()
  await expect(p.getByTestId('meet-after')).toContainText('ждём Настю')
  const stop = (await api(n, 'GET', '/inbox')).rows.find((r: any) => r.kind === 'stop')
  expect(stop.title).toContain('Вова предлагает после')
  await api(n, 'POST', `/proposals/${stop.id}/answer`, { yes: false })
  await expect(p.getByTestId('meet-after')).toContainText('Настя: нет', { timeout: 30000 })
  // the plans show the meeting; cancelling asks first and tells the partner
  await p.getByRole('button', { name: 'Назад' }).click()
  await p.getByTestId('tab-plans').click()
  await expect(p.getByTestId('plan-row')).toContainText('Планетарий')
  await shot(p, 'E55-plans')
  await p.getByTestId('plan-row').click()
  await p.getByTestId('cancel-meet').click()
  await expect(p.getByTestId('cancel-sheet')).toContainText('получит строку на главной')
  await p.getByTestId('cancel-yes').click()
  await expect(p.getByTestId('v2-plans')).toBeVisible()
  const rows = (await api(n, 'GET', '/inbox')).rows
  expect(rows.find((r: any) => r.kind === 'cancel').title).toBe('Вова отменил встречу: Планетарий на Баррикадной')
})

test('E56 планы: «Я занят» одним касанием; общее время находит окно и ведёт во встречу', async ({ browser }) => {
  test.setTimeout(120_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const [id] = await addIdeas(n, ['Каток в Парке Горького'])
  await api(n, 'POST', `/ideas/${id}/wish`, { wish: 'want' })
  await api(v, 'POST', `/ideas/${id}/wish`, { wish: 'want' })
  const f = await api(n, 'GET', '/free')
  const day = f.days[1].day
  await api(n, 'POST', '/free', { day, part: 2, on: true })
  const p = await openV2(await phone(browser), v, 'plans')
  await expect(p.getByTestId('v2-plans')).not.toContainText('Дела')
  await p.locator(`[data-day="${f.today}"]`).click()
  await p.getByTestId('busy-toggle').click()
  await expect(p.getByTestId('busy-toggle')).toHaveAttribute('aria-checked', 'true')
  expect((await api(n, 'GET', '/free')).partner_busy).toContain(f.today)
  await shot(p, 'E56-busy')
  await p.getByTestId('to-free').click()
  await expect(p.getByRole('button', { name: 'Назад' })).toContainText('Планы')
  await expect(p.getByTestId('v2-free')).toContainText('Общего пока нет')
  await p.locator(`[data-slot="${day}:2"]`).click()
  await expect(p.getByTestId('common')).toContainText('Оба свободны')
  await shot(p, 'E56-common')
  await p.getByTestId('common').getByRole('button', { name: 'Идём' }).click()
  await expect(p.getByTestId('meet-status')).toHaveText('ждём Настю')
  const rows = (await api(n, 'GET', '/inbox')).rows
  expect(rows[0].title).toBe('Вова зовёт: Каток в Парке Горького')
  // one call, one message: the proposal with the time, not «зовёт» and then the time
  await waitMsg(n, /Вова зовёт: (<b>)?Каток в Парке Горького(<\/b>)?\n/)
  expect((await chat(n)).filter(m => /Вова[\s\S]*Каток в Парке Горького/.test(m.text))).toHaveLength(1)
  await noMsg(n, /Время ещё не выбрано|Осталось найти время/)
})

test('E57 дела: своё на сегодня, просьба партнёру и отказ, повтор, сделанные и история', async ({ browser }) => {
  test.setTimeout(150_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const p = await openV2(await phone(browser), v, 'chores')
  // an empty tab offers one way on; a chore for me, for today
  await expect(p.getByTestId('v2-chores')).toContainText('Здесь дел нет')
  await p.getByTestId('add-chore').click()
  const sheet = p.getByTestId('chore-sheet')
  await sheet.getByPlaceholder('Забрать торт').fill('Купить билеты на поезд')
  await sheet.getByRole('button', { name: 'Сегодня' }).click()
  await p.getByTestId('chore-save').click()
  await expect(p.getByTestId('chore-row')).toContainText('Купить билеты на поезд')
  await expect(p.getByTestId('chore-row')).toContainText('сегодня')
  // a request to the partner: «ждём ответа»; she declines; it is back with two ways out
  await p.getByTestId('add-chore').click()
  await sheet.getByPlaceholder('Забрать торт').fill('Забрать торт')
  await sheet.getByRole('button', { name: 'Настя', exact: true }).click()
  await p.getByTestId('chore-save').click()
  await expect(p.getByTestId('chore-ask')).toContainText('ждём ответа Насти')
  await shot(p, 'E57-ask')
  const q = await openV2(await phone(browser), n, 'chores')
  await expect(q.getByTestId('chore-ask')).toContainText('Вова просит')
  await expect(q.getByTestId('chore-row').locator('.ck')).toBeDisabled() // the partner's chore is not ticked for him
  await q.getByTestId('chore-ask').getByRole('button', { name: 'Не могу' }).click()
  await expect(p.getByTestId('chore-ask')).toContainText('Настя не может', { timeout: 30000 })
  await p.getByTestId('chore-ask').getByRole('button', { name: 'Беру сам' }).click()
  await expect(p.getByTestId('chore-ask')).toHaveCount(0)
  await expect(p.getByTestId('chore-row')).toHaveCount(2)
  // the card: buttons by role; a monthly repeat brings the next one when ticked
  await p.getByTestId('chore-row').filter({ hasText: 'Купить билеты' }).locator('span').first().click()
  await expect(p.getByRole('button', { name: 'Назад' })).toContainText('Дела')
  await expect(p.getByTestId('act-done')).toBeVisible()
  await expect(p.getByTestId('act-pass')).toContainText('Передать Насте')
  await p.getByTestId('edit-chore').click()
  await sheet.getByRole('button', { name: 'каждый месяц' }).click()
  await p.getByTestId('chore-save').click()
  await p.getByTestId('act-done').click()
  await expect(p.getByTestId('toast')).toContainText('Сделано')
  await p.getByRole('button', { name: 'Назад' }).click()
  await expect(p.getByTestId('chore-row').filter({ hasText: 'Купить билеты' })).toContainText('каждый месяц')
  await p.getByTestId('done-toggle').click()
  await expect(p.getByTestId('chore-row').filter({ hasText: 'Вова ·' })).toContainText('Купить билеты')
  await shot(p, 'E57-done')
  // the home screen has one line about chores; the history knows who did what
  await p.getByTestId('tab-home').click()
  await expect(p.getByTestId('tile-chores')).toContainText('Дела')
  const h = await api(v, 'GET', '/chores/history?q=' + encodeURIComponent('билеты'))
  expect(h.chores[0]).toMatchObject({ title: 'Купить билеты на поезд', done_by: 'Вова' })
})

test('E58 профиль: история дел с поиском; настройки группами, общую видит партнёр', async ({ browser }) => {
  test.setTimeout(120_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await api(v, 'POST', '/chores', { title: 'Починить кран', who: 'me' })
  const id = (await api(v, 'GET', '/chores')).chores[0].id
  await api(v, 'POST', `/chores/${id}/done`)
  const p = await openV2(await phone(browser), v)
  await p.getByLabel('Профиль').click()
  await expect(p.getByTestId('v2-us')).toContainText('Вова и Настя')
  await p.getByTestId('hist-chores').click()
  await expect(p.getByRole('button', { name: 'Назад' })).toContainText('Профиль')
  await expect(p.getByTestId('hist-chore')).toContainText('Починить кран')
  await p.getByTestId('hist-search').fill('торт')
  await expect(p.getByTestId('v2-history')).toContainText('Ничего не нашлось')
  await p.getByTestId('hist-search').fill('кран')
  await expect(p.getByTestId('hist-chore')).toContainText('Вова')
  await shot(p, 'E58-history')
  await p.getByRole('button', { name: 'Назад' }).click()
  await p.getByTestId('to-settings').click()
  await expect(p.getByTestId('sec-notify')).toContainText('включено 5 из 6')
  await expect(p.getByTestId('sec-meet')).toContainText('до 5 000 ₽')
  await shot(p, 'E58-settings')
  // mine: a switch; the couple's: a choice the partner is told about
  await p.getByTestId('sec-notify').click()
  await p.getByTestId('set-notify.scout').click()
  await expect(p.getByTestId('set-notify.scout')).toHaveAttribute('aria-checked', 'true')
  await p.getByRole('button', { name: 'Назад' }).click()
  await expect(p.getByTestId('sec-notify')).toContainText('включено 6 из 6')
  await p.getByTestId('sec-meet').click()
  await p.getByTestId('set-meet.budget-3000').click()
  await expect(p.getByTestId('toast')).toContainText('Настя увидит изменение')
  const rows = (await api(n, 'GET', '/inbox')).rows
  expect(rows[0]).toMatchObject({ role: 'notice', sub: 'до 3 000 ₽' })
  expect((await api(n, 'GET', '/prefs')).pair['meet.budget']).toBe('3000')
  expect((await api(n, 'GET', '/prefs')).me['notify.scout']).toBe('0')
})

test('E59 помощник в новой оболочке отвечает карточками; дело, написанное боту, появляется во вкладке «Дела»', async ({ browser }) => {
  test.setTimeout(120_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(n, ['Ужин в грузинском ресторане'])
  const p = await openV2(await phone(browser), v, 'assistant')
  await expect(p.getByTestId('tab-assistant')).toHaveAttribute('aria-current', 'page')
  await p.getByTestId('chat-input').fill('куда поужинать?')
  await p.getByTestId('chat-send').click()
  await expect(p.getByTestId('chat-reply').last()).toBeVisible({ timeout: 30000 })
  await shot(p, 'E59-chat')
  await say(v, 'Купить молоко')
  await waitMsg(v, /Купить молоко/)
  await p.getByTestId('tab-chores').click()
  await expect(p.getByTestId('chore-row')).toContainText('Купить молоко', { timeout: 15000 })
  await press(v, '✓ Сделано')
  await expect.poll(async () => (await api(v, 'GET', '/chores/history')).chores.length, { timeout: 15000 }).toBe(1)
})

test('E60 «Мой вкус» в новой оболочке: одно касание отмечает сферу и открывает её подробности', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const p = await openV2(await phone(browser), v, 'us')
  await p.getByTestId('to-taste').click()
  await expect(p.getByRole('button', { name: 'Назад' })).toContainText('Профиль')
  const first = p.locator('[data-testid^="sphere-"]').first()
  const id = (await first.getAttribute('data-testid'))!.replace('sphere-', '')
  await first.click()
  await expect(first).toHaveAttribute('aria-pressed', 'true')
  await expect.poll(async () => (await api(v, 'GET', '/taste')).me.spheres?.[id] || 0).toBeGreaterThan(0)
  await shot(p, 'E60-taste')
})

test('E61 бот в новом флоу: «зовёт» без слотов, старые адреса кнопок открывают новые экраны, «Да» ставит встречу в планы', async ({ browser }) => {
  test.setTimeout(150_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const [id] = await addIdeas(n, ['Планетарий на Баррикадной'])
  await api(n, 'POST', `/ideas/${id}/wish`, { wish: 'want' })
  await api(v, 'POST', `/ideas/${id}/wish`, { wish: 'want' })
  // both have the new shell open: it tells the server, and the bot follows the new flow
  const ctxN = await phone(browser)
  const q = await openV2(ctxN, n)
  const p = await openV2(await phone(browser), v, 'idea/' + id)
  for (const u of [v, n]) expect((await api(u, 'POST', '/ui', { v: 2 })).ok).toBe(true)
  await p.getByTestId('go-meet').click()
  await expect(p.getByTestId('meet-status')).toHaveText('без времени')
  const rid = Number(p.url().match(/meet\/(\d+)/)![1])
  const call = await waitMsg(n, /Вова зовёт: (<b>)?Планетарий на Баррикадной(<\/b>)?\. Время ещё не выбрано/)
  const keys = (call.reply_markup?.inline_keyboard || []).flat()
  expect(keys.map((k: any) => k.web_app?.url || '').join(' ')).toContain(`/app/meet/${rid}`)
  expect(keys.filter((k: any) => (k.callback_data || '').startsWith('ts:') || (k.callback_data || '').startsWith('x:'))).toHaveLength(0)
  await noMsg(n, /отметь, когда тебе удобно|Когда тебе удобно/)
  await noMsg(v, /Решено|Осталось найти время/)
  // the old addresses of the bot's buttons open the screens that took their place, never the stub
  const dev = new URLSearchParams({ dev: `${n.id}:${n.name}`, devtoken: TOKEN, nointro: '1' })
  const old = async (path: string, testid: string) => {
    await q.goto(`/app/${path}?${dev}`)
    await expect(q.getByTestId(testid)).toBeVisible({ timeout: 10000 })
    await expect(q.getByTestId('v2')).not.toContainText('Экран собирается')
  }
  await old('round/' + rid, 'v2-meet')
  await expect(q.getByTestId('meet-status')).toHaveText('без времени')
  expect(q.url()).toContain('/app/meet/' + rid)
  await old('calendar', 'v2-plans')
  expect(q.url()).toContain('/app/plans')
  await old('ideas/scout', 'v2-ideas')
  await old('ideas/both', 'v2-ideas')
  await expect(q.getByTestId('match-row')).toContainText('Планетарий')
  await old('ideas/chores', 'v2-chores')
  await old('triage', 'v2-home')
  await old('pick/1', 'v2-pick')
  await old('us', 'v2-us')
  await old('taste', 'v2-taste')
  await old('idea/' + id, 'v2-idea')
  await shot(q, 'E61-old-link')
  // the time is a proposal: «Да» in the chat puts the meeting into the plans, and nothing of the old choice comes
  const f = await api(v, 'GET', '/free')
  await api(v, 'POST', `/meet/${rid}/time`, { day: f.days[2].day, part: 1 })
  const offer = await waitMsg(n, /Вова зовёт: (<b>)?Планетарий на Баррикадной(<\/b>)?\n/, 15000, call.message_id)
  expect((offer.reply_markup?.inline_keyboard || []).flat().map((k: any) => k.text)).toEqual(expect.arrayContaining(['Да', 'Не могу']))
  await press(n, 'Да')
  await waitMsg(n, /Встреча в планах/)
  await waitMsg(v, /Встреча в планах/)
  expect((await api(v, 'GET', `/rounds/${rid}`)).round.state).toBe('planned')
  for (const u of [v, n]) await noMsg(u, /Мы идём|Куда идём|Подтверди|предлагает время|Пока не совпало|ответила «да»/)
  await expect(p.getByTestId('meet-status')).toHaveText('в планах', { timeout: 30000 })
  // going back to the old screens gives the old messages back
  await api(n, 'POST', '/ui', { v: 1 })
  await api(v, 'POST', `/meet/${rid}/cancel`)
  await waitMsg(n, /Вова отменил выбор/)
})

// the author of the link has the new interface: /start gives the invitation, the code is in the link
async function inviteV2(a: U) {
  await say(a, '/start')
  const inv = await waitMsg(a, /Пригласи партнёра/)
  const code = inv.text.match(/start=inv_([A-Z0-9]+)/)![1]
  await api(a, 'POST', '/ui', { v: 2 })
  return code
}
const keys = (m: { reply_markup?: any }) => (m.reply_markup?.inline_keyboard || []).flat()
// the same phone opens another address of the app (a button of the bot pressed later)
async function reopen(p: Page, u: U, path: string) {
  const q = new URLSearchParams({ dev: `${u.id}:${u.name}`, devtoken: TOKEN, nointro: '1' })
  await p.goto(`/app/${path}?${q}`)
  await expect(p.getByTestId('v2')).toBeVisible({ timeout: 10000 })
  await expect(p.locator('.splash')).toHaveCount(0, { timeout: 6000 })
}

test('E62 приглашение: партнёр видит, кто зовёт, пара создаётся по «Принять», идеи переезжают, автор узнаёт', async ({ browser }) => {
  test.setTimeout(120_000)
  const [v, n] = people('Вова', 'Настя')
  const code = await inviteV2(v)
  await addIdeas(n, ['Бранч на Патриках']) // she has tried the app alone
  await say(n, '/start inv_' + code)
  const m = await waitMsg(n, /Привет, Настя! Вова зовёт тебя во «Вдвоём»: вместе выбирать, куда сходить, и делить дела\./)
  const open = keys(m).find((k: any) => k.text === 'Посмотреть приглашение')
  expect(open?.web_app?.url).toContain('/app/invite/' + code)
  // nothing is joined by opening the link
  expect((await api(n, 'GET', '/home')).paired).toBe(false)
  await noMsg(v, /с тобой/)
  const p = await openV2(await phone(browser), n, 'invite/' + code)
  const scr = p.getByTestId('v2-invite')
  await expect(scr).toContainText('Вова зовёт тебя во «Вдвоём»')
  for (const t of ['Отмечают, куда хочется', 'Предлагают время и место', 'Делят дела', 'Твои идеи переедут в пару вместе с тобой'])
    await expect(scr).toContainText(t)
  await expect(p.getByTestId('invite-later')).toHaveText('Не сейчас')
  await shot(p, 'E62-invite')
  await p.getByTestId('invite-accept').click()
  await expect(p.getByTestId('toast')).toContainText('Вы в паре. Твои идеи переехали')
  await expect(p.getByTestId('v2-home')).toBeVisible({ timeout: 15000 })
  await shot(p, 'E62-joined')
  // the author: a line in the bot and the same on the home screen; her idea is theirs now
  await waitMsg(v, /Настя приняла приглашение/)
  const hv = await api(v, 'GET', '/home')
  expect(hv.paired).toBe(true)
  expect(hv.partner.name).toBe('Настя')
  expect((await api(v, 'GET', '/ideas')).ideas.map((i: any) => i.title)).toContain('Бранч на Патриках')
  const pv = await openV2(await phone(browser), v)
  const row = pv.getByTestId('inbox-row').first()
  await expect(row).toContainText('Настя приняла приглашение')
  await shot(pv, 'E62-author')
  await row.getByRole('button', { name: 'Понятно' }).click()
  await expect(pv.getByTestId('inbox-row')).toHaveCount(0)
  // the link has worked once: the next one who opens it is told so, with no way in
  const [x] = people('Третий', '-')
  await say(x, '/start inv_' + code)
  const used = await waitMsg(x, /Вова уже в паре/)
  expect(keys(used)).toHaveLength(0)
  expect((await api(x, 'GET', '/home')).paired).toBe(false)
  const px = await openV2(await phone(browser), x, 'invite/' + code)
  await expect(px.getByTestId('invite-stop')).toContainText('Вова уже в паре')
  await expect(px.getByTestId('invite-accept')).toHaveCount(0)
  await shot(px, 'E62-used')
  // someone who is in a pair opens an invitation
  const after = await lastId(n)
  await say(n, '/start inv_' + code)
  await waitMsg(n, /Ты в паре с Вовой/, 15000, after)
  await reopen(p, n, 'invite/' + code)
  await expect(p.getByTestId('invite-stop')).toContainText('Ты в паре с Вовой')
})

test('E63 приглашение: «Не сейчас», своя ссылка, устаревшая ссылка и «Попросить новую»', async ({ browser }) => {
  test.setTimeout(120_000)
  const [v, n] = people('Вова', 'Настя')
  const code = await inviteV2(v)
  // the author opens their own link
  await say(v, '/start inv_' + code)
  await waitMsg(v, /Это твоя ссылка/)
  expect((await api(v, 'GET', '/invite/' + code)).state).toBe('own')
  // «Не сейчас»: she stays alone, on her own home screen, and the link still works
  await say(n, '/start inv_' + code)
  await waitMsg(n, /Вова зовёт тебя во «Вдвоём»/)
  const p = await openV2(await phone(browser), n, 'invite/' + code)
  await p.getByTestId('invite-later').click()
  await expect(p.getByTestId('v2-solo')).toBeVisible()
  expect((await api(n, 'GET', '/invite/' + code)).state).toBe('ok')
  // three days later the link has run out
  await advance({ minutes: 73 * 60 })
  const after = await lastId(n)
  await say(n, '/start inv_' + code)
  const old = await waitMsg(n, /Ссылка устарела/, 15000, after)
  expect(keys(old).map((k: any) => k.text)).toEqual(['Попросить новую'])
  await reopen(p, n, 'invite/' + code)
  await expect(p.getByTestId('invite-stop')).toContainText('Ссылка устарела')
  await expect(p.getByTestId('invite-accept')).toHaveCount(0)
  await shot(p, 'E63-expired')
  await p.getByTestId('invite-ask').click()
  await expect(p.getByTestId('toast')).toContainText('Вова получит просьбу прислать новую ссылку')
  await expect(p.getByTestId('invite-stop')).toContainText('На главную')
  // the author is asked in the bot and on the home screen, once
  const ask = await waitMsg(v, /Настя просит новую ссылку/)
  expect(keys(ask).map((k: any) => k.text)).toEqual(['Новая ссылка'])
  expect((await api(v, 'GET', '/home')).inbox[0].title).toBe('Настя просит новую ссылку')
  await press(n, 'Попросить новую')
  expect((await chat(v)).filter(m => /просит новую ссылку/.test(m.text))).toHaveLength(1)
  // «Новая ссылка» gives a link with the text of the project, and it works
  const before = await lastId(v)
  await press(v, 'Новая ссылка')
  const fresh = await waitMsg(v, /Пригласи партнёра/, 15000, before)
  const share = decodeURIComponent(keys(fresh).find((k: any) => k.text === 'Отправить приглашение').url)
  expect(share).toContain('Вова зовёт тебя во «Вдвоём»: вместе выбирать, куда сходить, и делить дела')
  const code2 = fresh.text.match(/start=inv_([A-Z0-9]+)/)![1]
  await reopen(p, n, 'invite/' + code2)
  await p.getByTestId('invite-accept').click()
  await expect(p.getByTestId('toast')).toContainText('Вы в паре')
  await waitMsg(v, /Настя приняла приглашение/)
  expect((await api(n, 'GET', '/home')).partner.name).toBe('Вова')
})

test('E64 «как прошло»: «Не ходили» одного не отменяет встречу за двоих, оба ответа в истории', async () => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  for (const u of [v, n]) await api(u, 'POST', '/ui', { v: 2 })
  const [id] = await addIdeas(v, ['Каток в Парке Горького'])
  const f = await api(v, 'GET', '/free')
  const { round } = await api(v, 'POST', '/rounds/with/' + id, { day: f.days[1].day, part: 1 })
  const offer = (await api(n, 'GET', '/inbox')).rows[0]
  await api(n, 'POST', `/proposals/${offer.id}/answer`, { yes: true })
  const planned = (await api(v, 'GET', `/rounds/${round.id}`)).round
  expect(planned.state).toBe('planned')
  await advance({ to: planned.ends_at + 4 * 3600 })
  const one = (await api(v, 'POST', `/rounds/${round.id}/rate`, { answer: 'skip' })).round
  expect(one.state).toBe('planned') // his answer is his: nothing is cancelled for her
  expect(one.again).toBe('skip')
  const two = (await api(n, 'POST', `/rounds/${round.id}/rate`, { answer: 'like' })).round
  expect(two.state).toBe('done')
  const hist = (await api(v, 'GET', '/history')).meetings.find((m: any) => m.id === round.id)
  expect(hist).toMatchObject({ again: 'skip', partner_again: 'yes' })
})

test('E65 выход из пары: лист с последствиями, идеи остаются партнёру, копии у ушедшего, пара заново без дублей', async ({ browser }) => {
  test.setTimeout(150_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await api(v, 'POST', '/ui', { v: 2 })
  await api(n, 'POST', '/ui', { v: 2 })
  const [planet] = await addIdeas(v, ['Планетарий на Баррикадной'])
  await addIdeas(n, ['Бранч на Патриках'])
  await api(v, 'POST', `/ideas/${planet}/wish`, { wish: 'want' })
  await api(v, 'POST', '/chores', { title: 'Купить билеты на поезд', who: 'me' })
  const titles = async (u: U) => (await api(u, 'GET', '/ideas')).ideas.map((i: any) => i.title).sort()
  // someone in a pair opens another invitation: the way on is «Пара» in the settings
  const [x] = people('Третий', '-')
  const code = await inviteV2(x)
  const p = await openV2(await phone(browser), v, 'invite/' + code)
  await expect(p.getByTestId('invite-stop')).toContainText('Ты в паре с Настей')
  await expect(p.getByTestId('invite-stop')).toContainText('Чтобы принять, выйди из неё в настройках')
  await p.getByTestId('invite-settings').click()
  const scr = p.getByTestId('v2-pair')
  await expect(scr).toContainText('Настя')
  await expect(scr).toContainText(/в паре с \d+ [а-я]+/)
  // the way out is only through the sheet that says what will happen
  await p.getByTestId('leave').click()
  const sheet = p.getByTestId('leave-sheet')
  for (const t of ['Выйти из пары?', 'Общие идеи, встречи и история останутся у Насти', 'Ты получишь копию своих идей', 'Назначенные встречи отменятся, Настя узнает об этом'])
    await expect(sheet).toContainText(t)
  await shot(p, 'E65-sheet')
  await sheet.getByRole('button', { name: 'Остаться' }).click()
  await expect(p.getByTestId('leave-sheet')).toHaveCount(0)
  expect((await api(v, 'GET', '/home')).paired).toBe(true)
  await p.getByTestId('leave').click()
  await p.getByTestId('leave-confirm').click()
  await expect(p.getByTestId('toast')).toContainText('Ты больше не в паре. Копия твоих идей с тобой')
  await expect(p.getByTestId('v2-solo')).toBeVisible()
  await shot(p, 'E65-left')
  // he has a copy of what he added; she has everything, his idea too
  expect(await titles(v)).toEqual(['Планетарий на Баррикадной'])
  expect(await titles(n)).toEqual(['Бранч на Патриках', 'Планетарий на Баррикадной'])
  expect((await api(v, 'GET', '/chores')).chores.map((c: any) => c.title)).toEqual(['Купить билеты на поезд'])
  expect((await api(n, 'GET', '/chores')).chores.find((c: any) => c.title === 'Купить билеты на поезд')).toMatchObject({ who: 'any', state: 'open' })
  // she learns it in the bot and on the home screen, which is the one without a pair again
  await waitMsg(n, /Вова вышел из пары\.<\/b> Идеи и история остались у тебя|Вова вышел из пары\. Идеи и история остались у тебя/)
  const pn = await openV2(await phone(browser), n)
  await expect(pn.getByTestId('v2-solo')).toBeVisible()
  const row = pn.getByTestId('inbox-row').first()
  await expect(row).toContainText('Вова вышел из пары')
  await expect(row).toContainText('Идеи и история остались у тебя')
  await expect(pn.getByTestId('invite')).toHaveText('Пригласить')
  await shot(pn, 'E65-stayed')
  await row.getByRole('button', { name: 'Понятно' }).click()
  await expect(pn.getByTestId('inbox-row')).toHaveCount(0)
  // «Пара» without a pair has no way out, only the way to invite
  await pn.getByLabel('Профиль').click()
  await pn.getByTestId('to-settings').click()
  await expect(pn.getByTestId('sec-pair')).toContainText('партнёра пока нет')
  await pn.getByTestId('sec-pair').click()
  await expect(pn.getByTestId('v2-pair')).toContainText('Партнёра пока нет')
  await expect(pn.getByTestId('leave')).toHaveCount(0)
  // a pair again, with each other: nothing doubles, his mark and his chore are his again
  const link = (await api(n, 'POST', '/invite')).link as string
  await api(v, 'POST', `/invite/${link.match(/inv_([A-Z0-9]+)/)![1]}/accept`)
  expect((await api(v, 'GET', '/home')).partner.name).toBe('Настя')
  expect(await titles(v)).toEqual(['Бранч на Патриках', 'Планетарий на Баррикадной'])
  expect((await api(v, 'GET', `/ideas/${planet}`)).idea.my_wish).toBe('want')
  const again = (await api(v, 'GET', '/chores')).chores.filter((c: any) => c.title === 'Купить билеты на поезд')
  expect(again).toHaveLength(1)
  expect(again[0].who).toBe('me')
})

test('E66 данные: файл приходит в бот без чужого; удаление по слову «удалить», партнёр ничего не теряет, человек начинает как новый', async ({ browser }) => {
  test.setTimeout(150_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await api(v, 'POST', '/ui', { v: 2 })
  await api(n, 'POST', '/ui', { v: 2 })
  const [planet] = await addIdeas(v, ['Планетарий на Баррикадной'])
  await addIdeas(n, ['Бранч на Патриках'])
  await api(v, 'POST', `/ideas/${planet}/wish`, { wish: 'want' })
  await api(n, 'POST', `/ideas/${planet}/wish`, { wish: 'very' })
  await api(n, 'POST', '/chores', { title: 'Полить цветы', who: 'me' })
  const p = await openV2(await phone(browser), n)
  await p.getByLabel('Профиль').click()
  await p.getByTestId('to-settings').click()
  await expect(p.getByTestId('sec-data')).toContainText('скачать, удалить')
  await p.getByTestId('sec-data').click()
  await expect(p.getByRole('button', { name: 'Назад' })).toContainText('Настройки')
  // the file: hers only, sent to her chat with the bot
  await p.getByTestId('data-export').click()
  await expect(p.getByTestId('toast')).toContainText('Файл придёт в бот')
  const doc = await waitMsg(n, /Твои идеи и история/)
  expect(doc.method).toBe('sendDocument')
  expect(doc.file_name).toMatch(/^vdvoem-\d{4}-\d\d-\d\d\.json$/)
  expect(JSON.parse(doc.file!)['мои идеи'].map((i: any) => i['название'])).toEqual(['Бранч на Патриках'])
  expect(doc.file).not.toContain('Планетарий')
  await noMsg(v, /Твои идеи и история/)
  await shot(p, 'E66-data')
  // deleting: the sheet lists what goes, and the button wakes up only when the word is typed
  await p.getByTestId('data-delete').click()
  const sheet = p.getByTestId('wipe-sheet')
  for (const t of ['Удалить мои данные?', 'останутся у Вовы без твоего имени, отметок и оценок', 'Удалятся твои идеи и отметки', 'Вернуть нельзя'])
    await expect(sheet).toContainText(t)
  await expect(p.getByTestId('wipe-confirm')).toBeDisabled()
  await p.getByTestId('wipe-word').fill('удали')
  await expect(p.getByTestId('wipe-confirm')).toBeDisabled()
  await shot(p, 'E66-sheet')
  expect((await api(n, 'POST', '/data/delete', { confirm: 'да' }).catch(e => String(e)))).toContain('not_confirmed')
  expect((await api(n, 'GET', '/home')).paired).toBe(true)
  await p.getByTestId('wipe-word').fill('Удалить')
  await p.getByTestId('wipe-confirm').click()
  // she is a new person: the first screen, nothing of hers
  await expect(p.getByTestId('v2-solo')).toBeVisible({ timeout: 20000 })
  await waitMsg(n, /Твои данные удалены/)
  expect((await api(n, 'GET', '/home')).paired).toBe(false)
  expect((await api(n, 'GET', '/ideas')).ideas).toHaveLength(0)
  expect((await api(n, 'GET', '/chores')).chores).toHaveLength(0)
  await shot(p, 'E66-new')
  // the partner: told without her name; every idea is his, her mark is gone, his stays; her chore is for anyone
  const told = await waitMsg(v, /Партнёр вышел из пары и удалил свои данные/)
  expect(told.text).not.toContain('Настя')
  const hv = await api(v, 'GET', '/home')
  expect(hv.paired).toBe(false)
  expect(hv.inbox[0]).toMatchObject({ role: 'notice', title: 'Партнёр вышел из пары и удалил свои данные' })
  expect((await api(v, 'GET', '/ideas')).ideas.map((i: any) => i.title).sort()).toEqual(['Бранч на Патриках', 'Планетарий на Баррикадной'])
  expect((await api(v, 'GET', `/ideas/${planet}`)).idea).toMatchObject({ my_wish: 'want', partner_marked: false })
  expect((await api(v, 'GET', '/chores')).chores.find((c: any) => c.title === 'Полить цветы')).toMatchObject({ who: 'any', state: 'open' })
  // the bot answers her as it answers a new person
  const after = await lastId(n)
  await say(n, '/start')
  await waitMsg(n, /Пригласи партнёра/, 15000, after)
})

test('E67 помощник: под ответом «Все N списком», подборка открывается и возвращает в чат', async ({ browser }) => {
  test.setTimeout(120_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const space = (await api(v, 'GET', '/me')).space.id
  const today = new Date((await api(v, 'GET', '/calendar')).today + 'T12:00')
  const at = (k: number) => { const d = new Date(today.getTime() + k * 86400_000); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}T19:00` }
  const picks = [...Array(8)].map((_, k) => ({ title: `Концерт E67 номер ${k + 1}`, category: 'culture', starts: at(2 + k % 4), price_rub: 1000 + k * 300, why: 'живая музыка', score: 7, url: `https://afisha.example/e67/${space}/${k}`, image: '' }))
  await fetch(BASE + '/agent/picks', { method: 'POST', body: JSON.stringify({ space, picks }) })
  const p = await openV2(await phone(browser), v, 'assistant')
  await p.getByTestId('chat-input').fill('что есть на неделе?')
  await p.getByTestId('chat-send').click()
  const all = p.getByTestId('pick-open')
  await expect(all).toBeVisible({ timeout: 30000 })
  await expect(all).toContainText(/Все \d+ списком/)
  const total = Number((await all.innerText()).match(/Все (\d+)/)![1])
  expect(total).toBeGreaterThan(4)
  await shot(p, 'E67-chat')
  await all.click()
  await expect(p.getByTestId('v2-pick')).toBeVisible()
  await expect(p.getByTestId('pick-row')).toHaveCount(total)
  await expect(p.getByRole('button', { name: 'Назад' })).toContainText('Помощник')
  await expect(p.getByTestId('tab-assistant')).toHaveAttribute('aria-current', 'page')
  await shot(p, 'E67-pick')
  await p.getByTestId('pick-row').first().click()
  await expect(p.getByTestId('v2-idea')).toContainText('Концерт E67')
  await expect(p.getByRole('button', { name: 'Назад' })).toContainText('Подборка')
  await p.getByRole('button', { name: 'Назад' }).click()
  await p.getByRole('button', { name: 'Назад' }).click()
  await expect(p.getByTestId('v2-assistant')).toBeVisible()
  await expect(p.getByTestId('pick-open')).toBeVisible()
})

// a microphone for the test: the browser's recorder is replaced by one that gives a recording at once
const fakeMic = `(() => {
  navigator.mediaDevices.getUserMedia = async () => {
    if (window.__micDeny) throw new DOMException('denied', 'NotAllowedError')
    return { getTracks: () => [{ stop() { window.__micStopped = (window.__micStopped || 0) + 1 } }] }
  }
  window.MediaRecorder = class {
    constructor() { this.state = 'inactive'; this.mimeType = 'audio/webm' }
    start() { this.state = 'recording' }
    stop() { if (this.state !== 'recording') return; this.state = 'inactive'
      this.ondataavailable && this.ondataavailable({ data: new Blob([new Uint8Array(window.__recBytes || 4000)], { type: 'audio/webm' }) }); this.onstop && this.onstop() }
  }
})()`

test('E68 голос в помощнике: кнопки нет без сервера речи; запись, «Распознаю», ответ; отказ в доступе и «не расслышал» простыми словами', async ({ browser }) => {
  test.setTimeout(150_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(n, ['Ужин в грузинском ресторане'])
  // the server of this run has no speech: there is no button that would not work
  const plain = await openV2(await phone(browser), v, 'assistant')
  await expect(plain.getByTestId('chat-input')).toBeVisible()
  if (!(await api(v, 'GET', '/assistant')).voice) await expect(plain.getByTestId('chat-mic')).toHaveCount(0)
  // the same screen when the server can hear: its answer to the recording is played here by the test
  const ctx = await phone(browser)
  await ctx.addInitScript(fakeMic)
  let voiceAnswer: (file: string) => Promise<{ status: number; body: unknown }> = async () => ({ status: 200, body: { heard: '' } })
  let release: () => void = () => {}
  await ctx.route('**/api/assistant', async r => {
    if (r.request().method() !== 'GET') return r.fallback()
    const res = await r.fetch()
    await r.fulfill({ response: res, json: { ...(await res.json()), voice: true } })
  })
  await ctx.route('**/api/assistant/voice', async r => {
    const hold = new Promise<void>(ok => { release = ok })
    const file = r.request().postData() || ''
    await hold
    const a = await voiceAnswer(file)
    await r.fulfill({ status: a.status, json: a.body })
  })
  const p = await openV2(ctx, v, 'assistant')
  const mic = p.getByTestId('chat-mic')
  await expect(mic).toHaveAttribute('aria-label', 'Сказать голосом')
  const box = (await mic.boundingBox())!
  expect(Math.min(box.width, box.height)).toBeGreaterThanOrEqual(44)
  // no access: said in plain words, the field stays
  await p.evaluate(() => { (window as any).__micDeny = true })
  await mic.click()
  await expect(p.getByTestId('voice-err')).toContainText('Нет доступа к микрофону')
  await expect(p.getByTestId('chat-input')).toBeVisible()
  await shot(p, 'E68-denied')
  // recording: the field shows it, the same button stops it
  await p.evaluate(() => { (window as any).__micDeny = false })
  await mic.click()
  await expect(p.getByTestId('voice-err')).toHaveCount(0)
  await expect(p.getByTestId('voice-state')).toContainText('Слушаю')
  await expect(mic).toHaveAttribute('aria-label', 'Остановить запись')
  await expect(p.getByTestId('chat-send')).toHaveCount(0)
  await shot(p, 'E68-rec')
  // stopped: «Распознаю…» until the server answers, the buttons wait
  let sent = ''
  voiceAnswer = async file => { sent = file; const r = await api(v, 'POST', '/assistant', { text: 'куда поужинать?' }); return { status: 200, body: { heard: 'куда поужинать?', reply: r.reply } } }
  await mic.click()
  await expect(p.getByTestId('voice-state')).toHaveText('Распознаю…')
  await expect(mic).toBeDisabled()
  await shot(p, 'E68-wait')
  release()
  await expect(p.locator('.v-msg.me').last()).toHaveText('куда поужинать?', { timeout: 30000 })
  await expect(p.getByTestId('chat-reply').last()).toBeVisible()
  await expect(p.getByTestId('voice-state')).toHaveCount(0)
  await expect(p.getByTestId('chat-input')).toBeVisible()
  expect(sent).toContain('name="audio"; filename="rec.webm"') // the same upload the old screen makes
  expect(await p.evaluate(() => (window as any).__micStopped)).toBeGreaterThan(0) // the microphone is let go
  // nothing heard
  voiceAnswer = async () => ({ status: 200, body: { heard: '' } })
  await mic.click(); await mic.click(); release()
  await expect(p.getByTestId('voice-err')).toContainText('Не расслышал. Повтори или напиши текстом.')
  // the speech server failed
  voiceAnswer = async () => ({ status: 502, body: { error: 'voice_failed' } })
  await mic.click(); await expect(p.getByTestId('voice-err')).toHaveCount(0)
  await mic.click(); release()
  await expect(p.getByTestId('voice-err')).toContainText('Не получилось разобрать голос. Напиши текстом.')
  // a tap instead of speech is not sent at all
  await p.evaluate(() => { (window as any).__recBytes = 100 })
  await mic.click(); await mic.click()
  await expect(p.getByTestId('voice-err')).toContainText('Ничего не записалось')
  await expect(mic).toBeEnabled()
})

// a tap zone: the place where a finger lands counts for the control at least 21 px from its centre every way
const tapZone = (p: Page, sel: string) => p.locator(sel).first().evaluate(e => {
  const r = e.getBoundingClientRect(), cx = r.left + r.width / 2, cy = r.top + r.height / 2
  const hit = (x: number, y: number) => { const t = document.elementFromPoint(x, y); return !!t && (t === e || e.contains(t)) }
  return hit(cx, cy) && hit(cx - 21, cy) && hit(cx + 21, cy) && hit(cx, cy - 21) && hit(cx, cy + 21)
})

test('E69 проход по экранам: лист поверх панели и на прокрученном экране, главная не шире телефона, зоны нажатия от 44 px, помощник над панелью', async ({ browser }) => {
  test.setTimeout(150_000)
  const [v, n] = people('Вова', 'Анастасия')
  await pair(v, n)
  const [id] = await addIdeas(n, ['Планетарий на Баррикадной: ночная программа «Звёздное небо» для двоих'])
  await api(n, 'POST', `/ideas/${id}/wish`, { wish: 'want' })
  await api(v, 'POST', `/ideas/${id}/wish`, { wish: 'want' })
  await addIdeas(n, ['Каток в Парке Горького'])
  const f = await api(v, 'GET', '/free')
  const { round } = await api(v, 'POST', '/rounds/with/' + id, { day: f.days[1].day, part: 1 })
  const offer = (await api(n, 'GET', '/inbox')).rows.find((r: any) => r.kind === 'time')
  await api(n, 'POST', `/proposals/${offer.id}/answer`, { yes: true })
  await api(n, 'POST', '/chores', { title: 'Забрать торт', who: 'partner' })
  const p = await openV2(await phone(browser), v)
  // the home screen: a long name of the meeting is cut in its tile, nothing goes off the side
  await expect(p.getByTestId('tile-meet')).toContainText('Планетарий')
  const home = await p.getByTestId('v2-home').evaluate(e => ({ sw: e.scrollWidth, cw: e.clientWidth, over: e.scrollHeight - e.clientHeight }))
  expect(home.sw).toBeLessThanOrEqual(home.cw)
  expect(home.over).toBeLessThanOrEqual(0)
  const vw = p.viewportSize()!.width
  const tile = (await p.getByTestId('tile-chores').boundingBox())!
  expect(tile.x + tile.width).toBeLessThanOrEqual(vw)
  // small controls keep their look and still take a finger
  for (const sel of ['[aria-label="Добавить идею или дело"]', '[aria-label="Профиль"]', '.dtabs [role=tab]', '.dtabs .back', '[data-testid=inbox-row] .qb button', '[data-testid=tab-assistant]', '[data-testid=tab-plans]'])
    await expect.poll(() => tapZone(p, sel), { message: 'зона нажатия ' + sel }).toBe(true)
  // the meeting card scrolled to its end: the sheet covers the bottom panel and its last button is in sight
  await p.getByTestId('tile-meet').click()
  await expect(p.getByTestId('meet-status')).toHaveText('в планах')
  await expect.poll(() => tapZone(p, '.subh .back')).toBe(true) // the screen slides in first
  await p.getByTestId('v2-meet').evaluate(e => { e.scrollTop = e.scrollHeight })
  await p.getByTestId('cancel-meet').click()
  const sheet = p.getByTestId('cancel-sheet')
  await expect(sheet).toBeVisible()
  await p.waitForTimeout(700) // the sheet slides in
  const st = await sheet.evaluate(e => {
    const r = e.getBoundingClientRect(), nav = document.querySelector('.v2 .nav')!.getBoundingClientRect()
    const last = [...e.querySelectorAll('button')].pop()!, b = last.getBoundingClientRect()
    const top = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2)
    const onNav = document.elementFromPoint(nav.left + nav.width / 2, nav.top + nav.height / 2)
    return { bottom: r.bottom, top: r.top, vh: innerHeight, lastSeen: top === last, lastText: last.innerText, navCovered: !!onNav?.closest('.v-scrim'), masked: getComputedStyle(e.parentElement!.parentElement!).maskImage }
  })
  expect(st.lastText).toBe('Оставить')
  expect(st.lastSeen).toBe(true)
  expect(st.navCovered).toBe(true)
  expect(st.top).toBeGreaterThanOrEqual(0)
  expect(Math.round(st.bottom)).toBe(st.vh)
  expect(st.masked).toBe('none') // the fade at the bottom of a screen does not eat the sheet
  await shot(p, 'E69-sheet')
  await sheet.getByRole('button', { name: 'Оставить' }).click()
  await expect(sheet).toHaveCount(0)
  // the time grid: every slot takes a finger
  await p.getByRole('button', { name: 'Перенести' }).click()
  const slot = (await p.getByTestId('time-sheet').locator('.slot').first().boundingBox())!
  expect(slot.height).toBeGreaterThanOrEqual(44)
  await p.keyboard.press('Escape')
  // the assistant: the field stays above the bottom panel, the answer ends above the field
  await p.getByTestId('tab-assistant').click()
  await p.getByTestId('chat-input').fill('что у нас запланировано?')
  await p.getByTestId('chat-send').click()
  await expect(p.getByTestId('chat-reply').last()).toBeVisible({ timeout: 30000 })
  await p.getByTestId('chat-input').fill('куда сходить на выходных?')
  await p.getByTestId('chat-send').click()
  await expect(p.getByTestId('chat-reply')).toHaveCount(2, { timeout: 30000 })
  await p.waitForTimeout(300)
  const chat = await p.evaluate(() => {
    const c = document.querySelector('.v2 .v-composer')!.getBoundingClientRect(), nav = document.querySelector('.v2 .nav')!.getBoundingClientRect()
    const g = document.querySelector('.v2 .nav .ai .g')!.getBoundingClientRect(), log = document.querySelector('[data-testid=chat-log]')!
    return { composerBottom: c.bottom, navTop: Math.min(nav.top, g.top), atEnd: log.scrollHeight - log.scrollTop - log.clientHeight, logBottom: log.getBoundingClientRect().bottom, composerTop: c.top }
  })
  expect(chat.composerBottom).toBeLessThanOrEqual(chat.navTop)
  expect(chat.logBottom).toBeLessThanOrEqual(chat.composerTop)
  expect(chat.atEnd).toBeLessThan(2)
  const send = (await p.getByTestId('chat-send').boundingBox())!
  expect(Math.min(send.width, send.height)).toBeGreaterThanOrEqual(44)
  await shot(p, 'E69-chat')
})

test('E70 проход по сценариям: чужое дело «Забрать себе», повтор называет следующую дату, просрочено красным, афишу не правят, история не теряет «назад», без пары в планах зовут партнёра', async ({ browser }) => {
  test.setTimeout(150_000)
  const [v, n] = people('Вова', 'Анастасия')
  await pair(v, n)
  const now = Math.floor(Date.now() / 1000), d0 = Math.floor(new Date(new Date().toLocaleString('en-US', { timeZone: 'Europe/Moscow' })).setHours(0, 0, 0, 0) / 1000)
  await api(v, 'POST', '/chores', { title: 'Оплатить интернет', who: 'me', due: now - 2 * 86400, has_due: true })
  await api(v, 'POST', '/chores', { title: 'Купить корм', who: 'me', due: now + 3600, has_due: true, repeat: 'month' })
  await api(n, 'POST', '/chores', { title: 'Полить цветы', who: 'me' })
  void d0
  const p = await openV2(await phone(browser), v, 'chores')
  // «Просрочено» is red in the light theme too
  const over = p.locator('.blk > label', { hasText: 'Просрочено' })
  await expect(over).toBeVisible()
  const red = (c: string) => { const m = c.match(/\d+/g)!.map(Number); return m[0] > 150 && m[1] < 90 && m[2] < 90 }
  expect(red(await over.evaluate(e => getComputedStyle(e).color))).toBe(true)
  expect(red(await p.getByTestId('chore-row').filter({ hasText: 'Оплатить' }).locator('small').evaluate(e => getComputedStyle(e).color))).toBe(true)
  // the tick takes a finger; the whole row opens the card
  await expect.poll(() => tapZone(p, '[data-testid=chore-row] .ck')).toBe(true) // the screen slides in first
  // a repeat: the line says when the next one is
  await p.getByTestId('chore-row').filter({ hasText: 'Купить корм' }).locator('.ck').click()
  await expect(p.getByTestId('toast')).toContainText(/Сделано\. Следующее: \d+ [а-я]+/)
  await expect(p.getByTestId('v2-chores')).toBeVisible() // the tick did not open the card
  // the partner's chore: «Напомнить» and «Забрать себе», the name in the right case
  await p.getByTestId('chore-row').filter({ hasText: 'Полить цветы' }).click({ position: { x: 300, y: 6 } }) // the edge of the row, not the text
  await expect(p.getByTestId('v2-chore')).toContainText('Полить цветы')
  await expect(p.getByTestId('act-nudge')).toHaveText('Напомнить Анастасии')
  await expect(p.getByTestId('act-take')).toHaveText('Забрать себе')
  await p.getByTestId('act-take').click()
  await expect(p.getByTestId('toast')).toContainText('Дело твоё')
  expect((await api(v, 'GET', '/chores')).chores.find((c: any) => c.title === 'Полить цветы').who).toBe('me')
  // an event of the afisha has no author among the two: no «Изменить», no empty fields to fill
  const space = (await api(v, 'GET', '/me')).space.id
  const today = new Date((await api(v, 'GET', '/calendar')).today + 'T12:00'), d = new Date(today.getTime() + 2 * 86400_000)
  const starts = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}T19:00`
  await fetch(BASE + '/agent/picks', { method: 'POST', body: JSON.stringify({ space, picks: [{ title: 'Органный концерт E70', category: 'culture', starts, price_rub: 3000, why: 'под ваши вкусы', score: 7, url: `https://afisha.example/e70/${space}/1`, image: '', address: 'Малая Грузинская, 27' }] }) })
  await p.getByTestId('tab-ideas').click()
  await p.getByRole('tab', { name: 'Афиша' }).click()
  await p.getByTestId('afisha-row').filter({ hasText: 'Органный концерт E70' }).click()
  await expect(p.getByTestId('fields')).toContainText('Малая Грузинская')
  await expect(p.getByTestId('edit-idea')).toHaveCount(0)
  await expect(p.getByTestId('fields')).not.toContainText('не заполнено')
  // the two views of the history are one step: «назад» still leads to the profile
  await p.getByTestId('tab-home').click()
  await p.getByLabel('Профиль').click()
  await p.getByTestId('hist-chores').click()
  await p.getByRole('tab', { name: 'Встречи' }).click()
  await expect(p.getByTestId('v2-history')).toContainText('Встреч ещё не было')
  await expect(p.getByRole('button', { name: 'Назад' })).toContainText('Профиль')
  await p.getByRole('button', { name: 'Назад' }).click()
  await expect(p.getByTestId('v2-us')).toBeVisible()
  // what quiet hours mean is said where they are switched
  await p.getByTestId('to-settings').click()
  await p.getByTestId('sec-notify').click()
  await expect(p.getByTestId('quiet-note')).toContainText('с 23:00 до 9:00')
  await expect.poll(() => tapZone(p, '[data-testid="set-notify.quiet"]')).toBe(true) // the screen slides in first
  // alone: the plans do not offer «совпавшее» and common time, they lead to the invitation
  const [x] = people('Игорь', '-')
  const px = await openV2(await phone(browser), x, 'plans')
  await expect(px.getByTestId('v2-plans')).toContainText('Встречи появятся с партнёром')
  await expect(px.getByTestId('to-free')).toHaveCount(0)
  await px.getByRole('button', { name: 'Пригласить партнёра' }).click()
  await expect(px.getByTestId('invite')).toBeVisible()
})

test('E71 после встречи: «Как прошло?» ждёт на главной у каждого; второй отвечает и после ответа первого; в истории видны обе оценки; новичку не пишут «Ждём партнёра»', async ({ browser }) => {
  test.setTimeout(150_000)
  const [v, n] = people('Вова', 'Анастасия')
  await pair(v, n)
  for (const u of [v, n]) await api(u, 'POST', '/ui', { v: 2 })
  const [id] = await addIdeas(v, ['Каток в Парке Горького'])
  const f = await api(v, 'GET', '/free')
  const { round } = await api(v, 'POST', '/rounds/with/' + id, { day: f.days[1].day, part: 1 })
  const offer = (await api(n, 'GET', '/inbox')).rows[0]
  await api(n, 'POST', `/proposals/${offer.id}/answer`, { yes: true })
  const planned = (await api(v, 'GET', `/rounds/${round.id}`)).round
  const later = new Date(await advance({ to: planned.ends_at + 4 * 3600 }))
  // the phones live in the same time as the server
  const cv = await phone(browser), cn = await phone(browser)
  await cv.clock.setFixedTime(later); await cn.clock.setFixedTime(later)
  const p = await openV2(cv, v)
  const row = p.getByTestId('after-row')
  await expect(row).toContainText('Как прошло: Каток в Парке Горького?')
  const fit = await p.getByTestId('v2-home').evaluate(e => e.scrollHeight - e.clientHeight)
  expect(fit).toBeLessThanOrEqual(0)
  await shot(p, 'E71-home')
  await row.getByRole('button', { name: 'Понравилось' }).click()
  await expect(p.getByTestId('toast')).toContainText('Записал в историю')
  await expect(p.getByTestId('after-row')).toHaveCount(0)
  // his answer made the meeting «прошла»; she is still asked, on the home screen and in the card
  expect((await api(n, 'GET', `/rounds/${round.id}`)).round.state).toBe('done')
  const q = await openV2(cn, n)
  await expect(q.getByTestId('after-row')).toContainText('Как прошло: Каток в Парке Горького?')
  await q.getByTestId('after-row').locator('.qt').click()
  await expect(q.getByTestId('meet-status')).toHaveText('прошла')
  await expect(q.getByTestId('after')).toContainText('Как тебе?')
  await q.getByTestId('after').getByRole('button', { name: 'Не очень' }).click()
  await expect(q.getByTestId('after')).toContainText('Твой ответ: не очень')
  await expect(q.getByTestId('after')).toContainText('Вова: понравилось')
  // the history shows both marks whole, not cut by the edge
  await reopen(q, n, 'history/meets')
  const rates = q.getByTestId('hist-rates').first()
  await expect(rates).toHaveText('ты: не очень · Вова: понравилось')
  expect(await rates.evaluate(e => e.scrollWidth <= e.clientWidth)).toBe(true)
  await shot(q, 'E71-history')
  // someone who has just come has sent nothing yet: the card calls to invite, and says «Ждём» only after it
  const [x] = people('Игорь', '-')
  const cx = await phone(browser)
  const px = await openV2(cx, x)
  px.on('popup', pp => { pp.close().catch(() => {}) })
  await expect(px.getByTestId('v2-solo')).toContainText('Позови партнёра')
  await expect(px.getByTestId('invite')).toHaveText('Пригласить')
  await px.getByTestId('invite').click()
  await expect(px.getByTestId('v2-solo')).toContainText('Ждём партнёра')
  await expect(px.getByTestId('invite')).toHaveText('Отправить ещё раз')
})

// what the app told the server at this launch: {v: 2} from the new shell, {v: 1} from the old screens
const saidUI = (p: Page, v: number) => p.waitForRequest(r => r.url().endsWith('/api/ui') && r.method() === 'POST' && r.postDataJSON()?.v === v, { timeout: 15000 })
const stored = (p: Page) => p.evaluate(() => localStorage.getItem('vd-v2'))

test('E72 новый интерфейс по умолчанию: вход без параметров, ?v2=0 включает старый и запоминается, ?v2=1 возвращает; кнопки бота ведут на новые экраны', async ({ browser }) => {
  test.setTimeout(150_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const ctx = await phone(browser)
  const p = await ctx.newPage()
  p.on('pageerror', e => console.log('pageerror:', e.message))
  const dev = new URLSearchParams({ dev: `${n.id}:${n.name}`, devtoken: TOKEN, nointro: '1' })
  const isNew = async () => { await expect(p.getByTestId('v2')).toBeVisible({ timeout: 10000 }); await expect(p.locator('main.screen')).toHaveCount(0) }
  const isOld = async () => { await expect(p.locator('main.screen')).toBeVisible({ timeout: 10000 }); await expect(p.getByTestId('v2')).toHaveCount(0) }

  // no parameter, nothing remembered: the new interface, and the server hears it
  let said = saidUI(p, 2)
  await p.goto(`/app/?${dev}`)
  await isNew()
  await said
  await expect(p.getByTestId('v2-home')).toBeVisible()
  expect(await stored(p)).toBeNull()
  await shot(p, 'E72-default')

  // the bot writes to someone who said nothing in the new flow, and its buttons open the new screens
  const before = await lastId(n)
  await say(n, 'Планетарий на Баррикадной')
  const card = await waitMsg(n, /Сохранил в идеи[\s\S]*Планетарий/, 15000, before)
  await noMsg(n, /Сохранил в копилку/, before)
  const ideaURL = keys(card).find((k: any) => k.text === 'Открыть')?.web_app?.url as string
  expect(ideaURL).toMatch(/\/app\/idea\/\d+$/)
  const id = Number(ideaURL.match(/idea\/(\d+)/)![1])
  await p.goto(new URL(ideaURL).pathname) // the button of the bot, as Telegram opens it: no parameters
  await isNew()
  await expect(p.getByTestId('v2-idea')).toContainText('Планетарий на Баррикадной')
  await api(n, 'POST', `/ideas/${id}/wish`, { wish: 'want' })
  await api(v, 'POST', `/ideas/${id}/wish`, { wish: 'want' })
  const rid = (await api(v, 'POST', `/rounds/with/${id}`)).round.id
  const call = await waitMsg(n, /Вова зовёт: (<b>)?Планетарий на Баррикадной(<\/b>)?\. Время ещё не выбрано/)
  const meetURL = keys(call).find((k: any) => k.text === 'Открыть встречу')?.web_app?.url as string
  expect(meetURL).toContain(`/app/meet/${rid}`)
  await p.goto(new URL(meetURL).pathname)
  await isNew()
  await expect(p.getByTestId('v2-meet')).toBeVisible()
  await expect(p.getByTestId('meet-status')).toHaveText('без времени')
  await shot(p, 'E72-bot-meet')
  // an offered time: its button still names the old address («round/N»), which is the meeting too
  const f = await api(v, 'GET', '/free')
  await api(v, 'POST', `/meet/${rid}/time`, { day: f.days[2].day, part: 1 })
  const offer = await waitMsg(n, /Вова зовёт: (<b>)?Планетарий на Баррикадной(<\/b>)?\n/, 15000, call.message_id)
  const otherURL = keys(offer).find((k: any) => k.text === 'Другое время')?.web_app?.url as string
  expect(otherURL).toContain(`/app/round/${rid}`)
  await p.goto(new URL(otherURL).pathname)
  await isNew()
  await expect(p.getByTestId('v2-meet')).toBeVisible()
  expect(p.url()).toContain('/app/meet/' + rid)

  // ?v2=0: the old screens, the phone remembers it and the server is told
  said = saidUI(p, 1)
  await p.goto(`/app/?${dev}&v2=0`)
  await isOld()
  await said
  expect(await stored(p)).toBe('0')
  await shot(p, 'E72-old')
  await p.goto('/app/') // opened again with no parameter (the menu button of the bot)
  await isOld()
  await p.goto('/app/idea/' + id)
  await isOld()
  // a new tab of the same phone keeps the choice too
  const p2 = await ctx.newPage()
  await p2.goto(`/app/?${dev}`)
  await expect(p2.locator('main.screen')).toBeVisible({ timeout: 10000 })
  await expect(p2.getByTestId('v2')).toHaveCount(0)
  await p2.close()

  // ?v2=1 returns the new interface, for good
  said = saidUI(p, 2)
  await p.goto(`/app/?${dev}&v2=1`)
  await isNew()
  await said
  expect(await stored(p)).toBe('1')
  await p.goto('/app/')
  await isNew()
  await expect(p.getByTestId('v2-home')).toBeVisible()

  // «1» left on a phone from the time the new interface was opt-in is the new interface
  const kept = await phone(browser)
  await kept.addInitScript(() => { try { if (!localStorage.getItem('vd-v2')) localStorage.setItem('vd-v2', '1') } catch { /* no storage */ } })
  const pk = await kept.newPage()
  await pk.goto(`/app/?${dev}`)
  await expect(pk.getByTestId('v2')).toBeVisible({ timeout: 10000 })
  await expect(pk.locator('main.screen')).toHaveCount(0)

  // people of the old interface (files 01–19): the helper opens the old screens for them
  const [o] = oldPeople('Олег', '-')
  const po = await open(await phone(browser), o)
  await expect(po.getByTestId('v2')).toHaveCount(0)
  expect(await stored(po)).toBe('0')
  const was = await lastId(o)
  await say(o, 'Каток в Парке Горького')
  await waitMsg(o, /Сохранил в копилку/, 15000, was)
})

test('E73 билеты на главной: после «Да» оба видят «кто покупает?»; «Куплю я» оставляет строку только покупателю; «Куплены» её закрывает', async ({ browser }) => {
  test.setTimeout(120_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const [id] = await addIdeas(v, ['Концерт в Зарядье'])
  const f = await api(v, 'GET', '/free')
  const { round } = await api(v, 'POST', '/rounds/with/' + id, { day: f.days[2].day, part: 2 })
  const offer = (await api(n, 'GET', '/inbox')).rows[0]
  await api(n, 'POST', `/proposals/${offer.id}/answer`, { yes: true })
  const cv = await phone(browser), cn = await phone(browser)
  const p = await openV2(cv, v), q = await openV2(cn, n)
  for (const x of [p, q]) {
    await expect(x.getByTestId('tickets-row')).toContainText('Билеты: Концерт в Зарядье')
    await expect(x.getByTestId('tickets-row')).toContainText('кто покупает?')
  }
  const fit = await p.getByTestId('v2-home').evaluate(e => e.scrollHeight - e.clientHeight)
  expect(fit).toBeLessThanOrEqual(0)
  await shot(p, 'E73-who-buys')
  await p.getByTestId('tickets-row').getByRole('button', { name: 'Куплю я' }).click()
  await expect(p.getByTestId('tickets-row')).toContainText('Купить билеты: Концерт в Зарядье')
  await expect(q.getByTestId('tickets-row')).toHaveCount(0, { timeout: 20000 })
  expect((await api(n, 'GET', '/meet/' + round.id)).buying).toBe('partner')
  await shot(p, 'E73-buyer')
  await p.getByTestId('tickets-row').getByRole('button', { name: 'Куплены' }).click()
  await expect(p.getByTestId('tickets-row')).toHaveCount(0)
  expect((await api(n, 'GET', '/rounds/' + round.id)).round.tickets).toBe('partner')
})
