import { test, expect } from '@playwright/test'
import { people, say, press, reply, waitMsg, pair, open, phone, shot, api, chat, lastId, planMeeting, EMU, BASE } from './lib'

test('E23 «Где именно»: общая идея → ссылка ответом на карточку → подбор помощника → варианты обоих → «Берём» → место в календаре', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await say(v, 'Каток')
  const card = await waitMsg(v, /Где именно\?/)
  const buttons = JSON.stringify(card.reply_markup)
  expect(buttons).toContain('yandex.ru/maps/?text=')
  expect(buttons).toContain('Подобрать места')
  // a link sent as a reply to the card becomes a place option, parsed by the server
  await reply(v, card.message_id, EMU + '/_emu/place/katok')
  await waitMsg(v, /Принял ссылку/)
  await waitMsg(v, /Добавил вариант места к «Каток»: <b>Каток в Парке Горького<\/b>\nул\. Крымский Вал, 9/)
  await waitMsg(n, /Вова добавил вариант места к «Каток»/)
  // the assistant suggests concrete places, each with a map search and «проверь»
  const b1 = await lastId(v)
  await press(v, 'Подобрать места')
  const list = await waitMsg(v, /Вот что можно выбрать[\s\S]*Каток на ВДНХ/, 10000, b1)
  expect(JSON.stringify(list.reply_markup)).toContain('Берём: Каток на ВДНХ')
  const idea = (await api(v, 'GET', '/ideas')).ideas.find((i: any) => i.title === 'Каток')
  // Настя adds her own option in the app
  const np = await open(await phone(browser), n, 'idea/' + idea.id)
  await expect(np.getByTestId('place-option')).toHaveCount(4)
  await np.getByLabel('Добавить вариант места').fill('Каток в Измайлово')
  await np.getByLabel('Добавить вариант', { exact: true }).click()
  await expect(np.getByTestId('place-option')).toHaveCount(5)
  await expect(np.getByTestId('place-option').filter({ hasText: 'Каток в Измайлово' })).toContainText('ты добавила')
  await shot(np, 'E23-options')
  await waitMsg(v, /Настя добавила вариант места к «Каток»: <b>Каток в Измайлово/)
  // after the match: pick the place on the ticket → the calendar event carries the address
  const planned = await planMeeting(v, n)
  expect(planned.chosen.title).toBe('Каток')
  const vp = await open(await phone(browser), v, 'round/' + planned.id)
  await expect(vp.getByText('Место не выбрано')).toBeVisible()
  const before = await lastId(n)
  await vp.getByTestId('place-option').filter({ hasText: 'Каток в Парке Горького' }).getByRole('button', { name: 'Берём этот' }).click()
  await expect(vp.locator('.ticket').getByText('Каток в Парке Горького')).toBeVisible()
  await shot(vp, 'E23-ticket-place')
  await waitMsg(n, /Вова выбрал место для «Каток»: <b>Каток в Парке Горького/, 10000, before)
  const upd = await waitMsg(n, /Место обновлено/, 10000, before)
  const link = JSON.stringify(upd.reply_markup).match(/https?:[^"]+\/cal\/[0-9a-f]{36}\/\d+\.ics/)![0].replace(/^https?:\/\/[^/]+/, BASE)
  const ics = await (await fetch(link)).text()
  expect(ics.replace(/\r\n /g, '')).toMatch(/LOCATION:Каток в Парке Горького\\, ул\. Крымский Вал\\, 9/)
  const fresh = (await api(v, 'GET', `/rounds/${planned.id}`)).round
  expect(fresh.chosen.place.chosen).toBe(true)
})
