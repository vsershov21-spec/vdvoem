import { test, type Page } from '@playwright/test'
import { appendFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { people, pair, phone, open, api, addIdeas, say, fakeTelegram, BASE } from './lib'

// JOURNEYS: four whole stories, end to end, each screen captured with its text — the material for the flow
// review (where a path breaks, what is said twice, where there is no air). Like the arena, it never fails the
// build: a step that breaks is written down as a finding. Run alone: JOURNEYS=1 npx playwright test 98-journeys
test.skip(!process.env.JOURNEYS, 'review run, on demand')
test.use({ actionTimeout: 6000, navigationTimeout: 15000 })
const LOG = '.run/journeys.log'
let n = 0
const day = (k: number, hm = '19:00') => { const d = new Date(Date.now() + k * 86400_000); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}T${hm}` }

async function cap(p: Page, j: string, name: string, note = '') {
  await p.waitForTimeout(900)
  const id = `j${j}-${String(++n).padStart(2, '0')}-${name}`
  await p.screenshot({ path: `shots/${id}.png`, fullPage: true }).catch(() => {})
  const info = await p.evaluate(() => {
    const vis = (e: Element) => { const b = (e as HTMLElement).getBoundingClientRect(); return b.width > 0 && b.height > 0 }
    const buttons = [...document.querySelectorAll('button,a[href],[role=tab]')].filter(vis)
      .map(e => (e.getAttribute('aria-label') || (e as HTMLElement).innerText || '').replace(/\s+/g, ' ').trim()).filter(Boolean)
    return { url: location.pathname, h: document.documentElement.scrollHeight, text: (document.body.innerText || '').replace(/\n{3,}/g, '\n\n').slice(0, 2500), buttons: [...new Set(buttons)].slice(0, 50) }
  }).catch(e => ({ url: '?', h: 0, text: 'evaluate failed: ' + e, buttons: [] as string[] }))
  appendFileSync(LOG, `\n===== ${id}${note ? ' — ' + note : ''}\nurl: ${info.url}  height: ${info.h}px\nbuttons: ${info.buttons.join(' | ')}\n---\n${info.text}\n`)
}
async function step(name: string, f: () => Promise<unknown>) {
  try { await f() } catch (e: any) { appendFileSync(LOG, `\n!!!!! STEP FAILED: ${name}: ${String(e?.message || e).slice(0, 300)}\n`) }
}
const go = async (p: Page, path: string) => { await p.goto(`/app/${path}`); await p.waitForTimeout(700) }
const scout = async (space: number, list: [string, string, string, number][]) => {
  const picks = list.map(([title, category, why, d], k) => ({ title, category, starts: d ? day(d, k % 2 ? '19:00' : '14:00') : '', price_rub: 900 + k * 500, why, score: 5 + (k % 4), url: `https://afisha.example/j/${space}/${k}/${title.length}`, image: '' }))
  await fetch(BASE + '/agent/picks', { method: 'POST', body: JSON.stringify({ space, picks }) })
}
const ask = async (p: Page, q: string) => { await p.locator('textarea.composer__field').fill(q); await p.getByLabel('Отправить').click(); await p.waitForTimeout(2500) }

test('JOURNEYS: четыре полных истории', async ({ browser }) => {
  test.setTimeout(1_200_000)
  mkdirSync('.run', { recursive: true }); mkdirSync('shots', { recursive: true })
  writeFileSync(LOG, 'JOURNEYS ' + new Date().toISOString() + '\n')

  // ---------- 1. Первый день: только что поставили, банк пуст ----------
  const [v1, a1] = people('Дима', 'Аня')
  await step('j1', async () => {
    const ctx = await phone(browser); await fakeTelegram(ctx, { platform: 'ios', bio: false })
    const p = await open(ctx, v1, '', true)
    await cap(p, '1', 'intro', 'первый запуск')
    for (let i = 0; i < 4; i++) { const b = p.getByRole('button', { name: /Дальше|Далее|Начать|Поехали|Понятно/ }).first(); if (!(await b.isVisible().catch(() => false))) break; await b.click(); await cap(p, '1', 'intro-' + (i + 2)) }
    await go(p, ''); await cap(p, '1', 'home-alone', 'главная до пары')
    await pair(v1, a1)
    await go(p, ''); await cap(p, '1', 'home-paired-empty', 'пара есть, идей нет')
    await step('add', async () => { await p.getByLabel('Добавить идею').first().click(); await cap(p, '1', 'add', 'добавить идею'); await p.locator('.sheet textarea, .sheet input').first().fill('Сходить в кино на новый фильм'); await cap(p, '1', 'add-typed'); await p.keyboard.press('Escape') })
    await step('ask', async () => { await go(p, 'assistant'); await cap(p, '1', 'assistant-empty'); await ask(p, 'куда сходить в субботу вечером?'); await cap(p, '1', 'assistant-first') })
    const space = (await api(v1, 'GET', '/me')).space.id
    await scout(space, [['Кино в «Октябре»: премьера', 'culture', 'новинка недели', 2], ['Грузинский ресторан «Хачапури и вино»', 'food', 'уютно и недорого', 0], ['Каток на ВДНХ', 'sport', 'активно', 1], ['Стендап в баре', 'other', 'весело', 3], ['Выставка «Импрессионисты»', 'culture', 'живопись', 0]])
    await go(p, ''); await cap(p, '1', 'home-after-scout', 'пришли первые находки скаута')
    await go(p, 'ideas'); await cap(p, '1', 'ideas'); await go(p, 'calendar'); await cap(p, '1', 'calendar'); await go(p, 'us'); await cap(p, '1', 'us')
    await ctx.close()
  })

  // ---------- 2. Совпадение → время → встреча → билеты (оба телефона) ----------
  const [v2, a2] = people('Вова', 'Настя')
  await step('j2', async () => {
    await pair(v2, a2)
    const space = (await api(v2, 'GET', '/me')).space.id
    await addIdeas(v2, ['Ужин в грузинском ресторане', 'Прогулка по набережной'])
    await scout(space, [['Иммерсивный спектакль «Вернувшиеся»', 'culture', 'Настя хотела иммерсивный', 3], ['Квиз в баре «Мозгобойня»', 'other', 'весело', 2], ['Органный концерт при свечах', 'culture', 'романтично', 4], ['Скалодром Limestone', 'sport', 'новое', 0]])
    const nIdeas = (await api(a2, 'GET', '/triage')).ideas
    for (const i of nIdeas.slice(0, 3)) await api(a2, 'POST', `/ideas/${i.id}/wish`, { wish: 'want' })
    const ctx = await phone(browser); await fakeTelegram(ctx, { platform: 'ios', bio: false })
    const p = await open(ctx, v2)
    await cap(p, '2', 'home', 'главная: Настя отметила 3')
    await step('swipe', async () => {
      await p.getByRole('button', { name: 'Пропущу' }).click(); await cap(p, '2', 'after-skip')
      await p.getByRole('button', { name: 'Хочу', exact: true }).click(); await cap(p, '2', 'match', 'совпадение?')
      await p.getByRole('button', { name: 'Выбрать время' }).click(); await cap(p, '2', 'time', 'выбор времени после совпадения')
    })
    await step('round', async () => {
      const r = (await api(v2, 'GET', '/rounds/current')).round
      const k = r.days[2] + ':2'
      await api(v2, 'POST', `/rounds/${r.id}/slots`, { slots: [k] }); await cap(p, '2', 'time-sent')
      await api(a2, 'POST', `/rounds/${r.id}/slots`, { slots: [k] })
      await go(p, 'round/' + r.id); await cap(p, '2', 'round-after-both', 'оба отметили время')
      const r2 = (await api(v2, 'GET', `/rounds/${r.id}`)).round
      if (r2.state === 'vote' && r2.triple?.length) { await api(v2, 'POST', `/rounds/${r.id}/vote`, { idea_id: r2.triple[0].id }); await api(a2, 'POST', `/rounds/${r.id}/vote`, { idea_id: r2.triple[0].id }) }
      await api(v2, 'POST', `/rounds/${r.id}/confirm`).catch(() => {}); await api(a2, 'POST', `/rounds/${r.id}/confirm`).catch(() => {})
      await go(p, 'round/' + r.id); await cap(p, '2', 'planned', 'встреча запланирована')
      await go(p, ''); await cap(p, '2', 'home-planned', 'главная с встречей')
      await go(p, 'calendar'); await cap(p, '2', 'calendar-planned')
    })
    await ctx.close()
  })

  // ---------- 3. Помощник: как люди спрашивают на самом деле ----------
  await step('j3', async () => {
    const ctx = await phone(browser); await fakeTelegram(ctx, { platform: 'ios', bio: false })
    const p = await open(ctx, v2, 'assistant')
    for (const q of ['что сегодня в кино?', 'кинчик в эту субботу', 'а подешевле?', 'расскажи про первое', 'опять мимо', 'грузинские рестики', 'что-то с рыбами', 'что на новый год', 'сколько мы потратили', 'обо мне', 'не люблю стендап', 'удиви нас']) {
      await step('ask ' + q, async () => { await ask(p, q); await cap(p, '3', q.slice(0, 16).replace(/[^\p{L}\p{N}]+/gu, '_'), 'спросил: ' + q) })
    }
    await ctx.close()
  })

  // ---------- 4. Настя, тёмная тема: срочная просьба, календарь, карточка, «Мы» ----------
  await step('j4', async () => {
    await say(v2, 'Срочно хочу на органный концерт, пусть Настя ответит'); await new Promise(r => setTimeout(r, 1200))
    const ctx = await phone(browser, { dark: true }); await fakeTelegram(ctx, { platform: 'ios', bio: false, dark: true })
    const p = await open(ctx, a2)
    await cap(p, '4', 'home-dark', 'главная Насти')
    for (const [path, name] of [['ideas', 'ideas'], ['ideas/both', 'both'], ['ideas/scout', 'afisha'], ['calendar', 'calendar'], ['us', 'us'], ['taste', 'taste']] as const) await step(name, async () => { await go(p, path); await cap(p, '4', name) })
    await step('calendar afisha', async () => { await go(p, 'calendar'); await p.getByRole('tab', { name: 'Афиша' }).click(); await cap(p, '4', 'calendar-afisha') })
    await step('idea', async () => { const i = (await api(a2, 'GET', '/ideas')).ideas[0]; await go(p, 'idea/' + i.id); await cap(p, '4', 'idea', 'карточка идеи') })
    await step('calsub', async () => { await go(p, 'calendar'); await p.getByTestId('calsub').click(); await cap(p, '4', 'calsub', 'подключение календаря') })
    await ctx.close()
  })
})
