import { test, type Page, type BrowserContext } from '@playwright/test'
import { appendFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { people, pair, phone, open, api, addIdeas, planMeeting, say, chat, waitMsg, fakeTelegram, BASE } from './lib'

// ARENA: one couple's whole week in the app, every screen captured (light and dark, as each partner) with the text
// on it and both bot chats — the material for the UI/UX review. It never fails the build: a step that breaks is
// written down as a finding instead.
test.use({ actionTimeout: 6000, navigationTimeout: 15000 })
const LOG = '.run/arena.log', BOT = '.run/arena-bot.log'
let n = 0
const day = (d: number, hm = '19:00') => { const t = new Date(Date.now() + d * 86400_000); const [h, m] = hm.split(':'); t.setHours(+h, +m, 0, 0); return t.toISOString() }

async function cap(p: Page, name: string, note = '') {
  await p.waitForTimeout(900)
  const id = `arena-${String(++n).padStart(2, '0')}-${name}`
  await p.screenshot({ path: `shots/${id}.png`, fullPage: true }).catch(() => {})
  const info = await p.evaluate(() => {
    const vis = (e: Element) => { const b = (e as HTMLElement).getBoundingClientRect(); return b.width > 0 && b.height > 0 }
    const buttons = [...document.querySelectorAll('button,a[href],[role=tab]')].filter(vis)
      .map(e => (e.getAttribute('aria-label') || (e as HTMLElement).innerText || '').replace(/\s+/g, ' ').trim()).filter(Boolean)
    return { url: location.pathname + location.search.replace(/devtoken=[^&]+/, ''), h: document.documentElement.scrollHeight,
      text: (document.body.innerText || '').replace(/\n{3,}/g, '\n\n').slice(0, 3500), buttons: [...new Set(buttons)].slice(0, 60) }
  }).catch(e => ({ url: '?', h: 0, text: 'evaluate failed: ' + e, buttons: [] as string[] }))
  appendFileSync(LOG, `\n===== ${id}${note ? ' — ' + note : ''}\nurl: ${info.url}  page height: ${info.h}px\nbuttons: ${info.buttons.join(' | ')}\n---\n${info.text}\n`)
}
async function step(name: string, f: () => Promise<unknown>) {
  try { await f() } catch (e: any) { appendFileSync(LOG, `\n!!!!! STEP FAILED: ${name}: ${String(e?.message || e).slice(0, 400)}\n`) }
}
const go = async (p: Page, path: string) => { await p.goto(`/app/${path}`); await p.waitForTimeout(600) }

test('ARENA: неделя пары в приложении — все экраны для разбора', async ({ browser }) => {
  test.setTimeout(900_000)
  mkdirSync('.run', { recursive: true }); mkdirSync('shots', { recursive: true })
  writeFileSync(LOG, 'ARENA run ' + new Date().toISOString() + '\n')
  const [v, a] = people('Вова', 'Настя')

  // ---------- day 0: the first open, before anything exists ----------
  await step('first open', async () => {
    const ctx = await phone(browser); await fakeTelegram(ctx, { platform: 'ios', bio: false })
    const p = await open(ctx, v, '', true)
    await cap(p, 'intro', 'самый первый запуск, ещё без пары')
    for (let i = 0; i < 4; i++) { const b = p.getByRole('button', { name: /Дальше|Далее|Начать|Поехали|Понятно/ }).first(); if (!(await b.isVisible().catch(() => false))) break; await b.click(); await cap(p, 'intro-' + (i + 2)) }
    await ctx.close()
  })
  await pair(v, a)

  // ---------- the bank: what a couple really piles up ----------
  const ids = await addIdeas(v, ['Ужин в грузинском ресторане', 'Театр на Таганке — Мастер и Маргарита', 'Прогулка по набережной', 'Каток в Парке Горького',
    'Мастер-класс по керамике', 'Квест в реальности', 'Сходить в баню', 'Джаз в Эссе', 'Поездка в Суздаль', 'Помыть окна', 'Купить продукты на неделю'])
  const ids2 = await addIdeas(a, ['Иммерсивный спектакль', 'Кино под открытым небом', 'Стендап', 'Пикник в Коломенском'])
  await step('wishes', async () => {
    for (const [k, w] of [[0, 'very'], [1, 'want'], [3, 'want'], [4, 'very'], [6, 'skip'], [7, 'want']] as const) await api(v, 'POST', `/ideas/${ids[k]}/wish`, { wish: w })
    for (const [k, w] of [[0, 'want'], [1, 'very'], [2, 'want'], [4, 'want'], [5, 'skip']] as const) await api(a, 'POST', `/ideas/${ids[k]}/wish`, { wish: w })
    await api(a, 'POST', `/ideas/${ids2[0]}/wish`, { wish: 'very' })
  })
  // the scout's week and announcements
  await step('scout picks', async () => {
    const space = (await api(v, 'GET', '/me')).space.id
    const goal = (await api(a, 'POST', '/goals', { text: 'Щелкунчик в декабре' })).id
    const picks: any[] = [
      ['Премьера в театре Маяковского', 'culture', 'драма, премьера сезона'], ['Фестиваль грузинской кухни', 'food', 'вы оба любите грузинскую кухню'],
      ['Большой стендап-концерт', 'other', 'Настя добавила стендап'], ['Прогулка по ВДНХ с экскурсоводом', 'walk', 'спокойно и на воздухе'],
      ['Выставка импрессионистов в Пушкинском', 'culture', 'живопись'], ['Каток на ВДНХ', 'sport', 'активно, вдвоём'], ['Квиз в баре «Мозгобойня»', 'other', 'весело и недорого'],
      ['Органный концерт при свечах', 'culture', 'романтично'], ['Мастер-класс по пасте', 'food', 'готовить вместе'], ['Иммерсивный спектакль «Вернувшиеся»', 'culture', 'Настя хотела иммерсивный'],
      ['Скалодром Limestone', 'sport', 'попробовать новое'], ['Джазовый вечер в Эссе', 'culture', 'джаз в вашей копилке'],
    ].map(([title, category, why], k) => ({ title, category, starts: day(1 + (k % 6), k % 2 ? '19:00' : '14:00'), price_rub: 800 + k * 450, why, score: 5 + (k % 4), url: `https://afisha.example/arena/${space}/${k}`, image: '' }))
    picks.push({ title: 'Щелкунчик в Большом театре', category: 'culture', starts: day(40), price_rub: 9000, why: 'вы следите за Щелкунчиком', score: 9, url: `https://afisha.example/arena/${space}/nut`, image: '', horizon: 'later', on_sale: day(4, '10:00'), goal_id: goal })
    picks.push({ title: 'Земфира — большой концерт', category: 'culture', starts: day(120), price_rub: 7000, why: 'анонс', score: 7, url: `https://afisha.example/arena/${space}/zem`, image: '', horizon: 'later' })
    await fetch(BASE + '/agent/picks', { method: 'POST', body: JSON.stringify({ space, picks }) })
    await waitMsg(a, /Скаут нашёл/, 10000).catch(() => {})
  })
  // one meeting already agreed, one urgent wish
  await step('planned meeting', () => planMeeting(v, a))
  await step('urgent', async () => { await say(a, 'Срочно хочу на Мастера и Маргариту, билеты быстро кончаются'); await new Promise(r => setTimeout(r, 1500)) })

  // ---------- the app, as Вова (light) ----------
  const tour = async (ctx: BrowserContext, who: typeof v, s: string) => {
    const p = await open(ctx, who)
    await cap(p, 'home' + s, 'главная')
    await step('island', async () => { await p.getByTestId('island').click(); await cap(p, 'island-open' + s, 'раскрытый остров с быстрыми действиями'); await p.getByLabel('Свернуть').click() })
    for (const [path, name, note] of [['ideas', 'bank', 'банк идей'], ['ideas/both', 'folder-both', 'папка «хотим оба»'], ['ideas/unmarked', 'folder-unmarked', 'неотмеченные'],
      ['ideas/chores', 'folder-chores', 'дела'], ['ideas/scout', 'afisha', 'афиша / скаут'], ['triage', 'triage', 'разбор карточек по одной'],
      [`idea/${ids[1]}`, 'idea', 'карточка идеи'], [`idea/${ids[6]}`, 'idea-skipped', 'идея, которую один пропустил'],
      ['calendar', 'calendar', 'календарь / планы'], ['us', 'us', 'профиль пары']] as const) {
      await step(name, async () => { await go(p, path); await cap(p, name + s, note) })
    }
    await step('afisha later', async () => { await go(p, 'ideas/scout'); await p.getByRole('tab', { name: /Анонсы/ }).click(); await cap(p, 'afisha-later' + s, 'анонсы на потом') })
    return p
  }
  const ctxV = await phone(browser); await fakeTelegram(ctxV, { platform: 'ios', bio: false })
  const p = await tour(ctxV, v, '')

  await step('add sheet', async () => {
    await go(p, ''); await p.getByLabel('Добавить идею').first().click(); await cap(p, 'add-sheet', 'добавление идеи')
    await p.locator('.sheet textarea, .sheet input').first().fill('Сходить на хоккей ЦСКА'); await cap(p, 'add-sheet-typed', 'ввод идеи')
    await p.keyboard.press('Escape')
  })
  await step('surprise', async () => {
    await go(p, ''); await p.getByTestId('island').click(); await p.getByText('Удиви нас').first().click(); await cap(p, 'surprise', '«Удиви нас»')
    await p.keyboard.press('Escape')
  })
  // ---------- choosing together: a fresh round ----------
  await step('round', async () => {
    const cur = (await api(v, 'GET', '/home')).next
    if (cur) await api(v, 'POST', `/rounds/${cur.id}/cancel`).catch(() => {})
    const { round } = await api(v, 'POST', '/rounds')
    await go(p, 'round/' + round.id); await cap(p, 'round-1-time', 'выбор времени (раунд)')
    await p.locator('.wkchip, .slot, [data-slot]').nth(3).click().catch(() => {})
    await cap(p, 'round-1-time-picked', 'отметил слот')
    const k = round.days[2] + ':2'
    await api(v, 'POST', `/rounds/${round.id}/slots`, { slots: [k] }).catch(() => {})
    await api(a, 'POST', `/rounds/${round.id}/slots`, { slots: [k] }).catch(() => {})
    await go(p, 'round/' + round.id); await cap(p, 'round-2-vote', 'голосование за идею')
    const r2 = (await api(v, 'GET', `/rounds/${round.id}`)).round
    if (r2.triple?.length) { await api(v, 'POST', `/rounds/${round.id}/vote`, { idea_id: r2.triple[0].id }); await go(p, 'round/' + round.id); await cap(p, 'round-3-voted-me', 'я проголосовал, жду партнёра') ; await api(a, 'POST', `/rounds/${round.id}/vote`, { idea_id: r2.triple[0].id }); await go(p, 'round/' + round.id); await cap(p, 'round-4-match', 'совпали') }
    await api(v, 'POST', `/rounds/${round.id}/confirm`).catch(() => {}); await api(a, 'POST', `/rounds/${round.id}/confirm`).catch(() => {})
    await go(p, 'round/' + round.id); await cap(p, 'round-5-planned', 'встреча запланирована — билеты, календарь, маршрут')
  })
  // ---------- the assistant: how people actually ask ----------
  await step('assistant', async () => {
    await go(p, 'assistant'); await cap(p, 'assistant-empty', 'помощник: пустой чат')
    for (const q of ['что есть на выходных до 4000?', 'хотим на джаз', 'а подешевле?', 'удиви нас', 'покажи вообще всё что нашёл скаут', 'расскажи про Мастера и Маргариту', 'что у нас запланировано?']) {
      const f = p.locator('textarea.composer__field')
      await f.fill(q); await p.getByLabel('Отправить').click()
      await p.waitForTimeout(2500)
      await cap(p, 'assistant-' + q.slice(0, 18).replace(/[^\p{L}\p{N}]+/gu, '_'), 'спросил: ' + q)
    }
    await step('assistant typing', async () => { await p.locator('textarea.composer__field').fill('хоти'); await cap(p, 'assistant-typing', 'подсказки при наборе') })
  })
  await ctxV.close()

  // ---------- Настя: dark theme, the partner's side ----------
  const ctxA = await phone(browser, { dark: true }); await fakeTelegram(ctxA, { platform: 'ios', bio: false })
  await tour(ctxA, a, '-dark-nastya')
  await ctxA.close()

  // ---------- small screen: iPhone SE ----------
  await step('small screen', async () => {
    const ctx = await browser.newContext({ viewport: { width: 375, height: 667 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true, baseURL: BASE, locale: 'ru-RU', timezoneId: 'Europe/Moscow' })
    await fakeTelegram(ctx, { platform: 'ios', bio: false })
    const q = await open(ctx, v)
    await cap(q, 'se-home', 'iPhone SE: главная'); await go(q, 'ideas'); await cap(q, 'se-bank', 'iPhone SE: банк'); await go(q, 'assistant'); await cap(q, 'se-assistant', 'iPhone SE: помощник')
    await ctx.close()
  })

  // ---------- the bot chats, as each of them sees them ----------
  for (const u of [v, a]) {
    const c = await chat(u)
    appendFileSync(BOT, `\n########## бот глазами ${u.name} (${c.length} сообщений)\n` + c.map(m => `--- #${m.message_id} ${m.method}${m.silent ? ' (тихо)' : ''}\n${m.text}\n` +
      (m.reply_markup?.inline_keyboard ? 'кнопки: ' + m.reply_markup.inline_keyboard.map((r: any[]) => r.map(b => b.text).join(' · ')).join(' / ') + '\n' : '')).join(''))
  }
})
