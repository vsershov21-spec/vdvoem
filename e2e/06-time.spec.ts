// These scenarios move the server clock forward, so they run last.
import { test, expect } from '@playwright/test'
import { people, press, photo, waitMsg, noMsg, pair, open, phone, shot, api, addIdeas, planMeeting, advance, lastId, say } from './lib'

test('E14 молчание: напоминания, через 48 часов выбор закрыт', async () => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(v, ['Ужин в ресторане'])
  const { round } = await api(v, 'POST', '/rounds')
  await api(v, 'POST', `/rounds/${round.id}/slots`, { slots: [round.days[0] + ':2'] })
  const before = await lastId(n)
  await advance({ minutes: 13 * 60 })
  let got = (await (await fetch(process.env.EMU + '/_emu/chat/' + n.id)).json()).some((m: any) => m.message_id > before && /Напоминаю/.test(m.text))
  if (!got) await advance({ minutes: 10 * 60 }) // landed in the night: it comes in the morning
  await waitMsg(n, /Напоминаю: отметь, когда тебе удобно/, 8000, before)
  await noMsg(v, /Напоминаю: отметь/, 0) // the one who answered is not nagged
  // 48 hours after opening the round closes; the message may land in the quiet hours and wait for 09:00
  if (Date.parse(await advance({ minutes: 0 })) / 1000 < round.expires_at + 120) await advance({ to: round.expires_at + 120 })
  const closed = async () => (await (await fetch(process.env.EMU + '/_emu/chat/' + v.id)).json()).some((m: any) => /Выбор закрылся/.test(m.text))
  await new Promise(r => setTimeout(r, 2500))
  if (!(await closed())) await advance({ minutes: 10 * 60 })
  await waitMsg(v, /Выбор закрылся/, 8000)
  expect((await api(v, 'GET', `/rounds/${round.id}`)).round.state).toBe('expired')
})

test('E15+E21 после встречи: напоминание за 2 часа, тихие часы, «Сходили?», повторим, фото', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(v, ['Ужин в ресторане'])
  const r = await planMeeting(v, n)
  expect(new Date((r.starts_at + 3 * 3600) * 1000).getUTCHours()).toBeGreaterThanOrEqual(18) // Moscow evening
  await advance({ to: r.starts_at - 2 * 3600 + 60 })
  await waitMsg(v, /Через 2 часа: .*Ужин в ресторане/)
  await waitMsg(n, /Через 2 часа/)
  // the «how was it» question is due at end + 3h — that is night, so it waits for 09:00
  const before = await lastId(v)
  await advance({ to: r.ends_at + 3 * 3600 + 20 * 60 })
  await noMsg(v, /Как прошло/, before)
  await advance({ to: r.ends_at + 14 * 3600 })
  await waitMsg(v, /Как прошло — .*Ужин в ресторане.*Сходили\?/, 8000, before)
  await press(v, 'Да')
  await waitMsg(v, /Здорово, что сходили/)
  await photo(v)
  await waitMsg(v, /Сохранил фото/)
  await press(v, 'Да, ещё раз')
  await waitMsg(n, /Здорово, что сходили/)
  await press(n, 'Да, ещё раз')
  await waitMsg(v, /Повторим — идея вернулась/)
  const p = await open(await phone(browser), v, 'us')
  await expect(p.locator('.icard', { hasText: 'Ужин в ресторане' })).toContainText('повторим')
  await expect(p.getByText('Понравилось')).toBeVisible()
  await shot(p, 'E15-history')
})

test('E16 бот сам предлагает: среда 13:00 → «Другую» / «Рандом» / «Давай!»', async () => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(v, ['Ужин в ресторане', 'Музей космонавтики', 'Каток в парке'])
  await api(v, 'PATCH', '/settings', { suggest_mode: 'best' })
  const me = await api(v, 'GET', '/me')
  // next Wednesday 13:05 Moscow time, from the server's clock
  const msk = new Date((me.now + 3 * 3600) * 1000)
  const days = (3 - msk.getUTCDay() + 7) % 7
  const target = Date.UTC(msk.getUTCFullYear(), msk.getUTCMonth(), msk.getUTCDate() + days, 13 - 3, 5) / 1000
  const before = await lastId(v)
  await advance({ to: target <= me.now ? target + 7 * 86400 : target })
  await waitMsg(v, /Как насчёт этого на выходных\?/, 8000, before)
  await waitMsg(n, /Как насчёт этого на выходных\?/, 8000, before)
  const b1 = await lastId(v)
  await press(v, 'Другую')
  await waitMsg(v, /Как насчёт/, 8000, b1)
  await press(v, 'Рандом')
  await waitMsg(v, /Случайная идея из вашей копилки/, 8000)
  await press(v, 'Давай!')
  await waitMsg(n, /Вова хочет: /)
  expect((await api(v, 'GET', '/rounds/current')).round.chosen).toBeTruthy()
  await say(v, '/surprise')
  await waitMsg(v, /Случайная идея|Как насчёт/)
})
