import { test, expect } from '@playwright/test'
import { people, pair, phone, open, shot, api, addIdeas, waitMsg, press, chat, BASE } from './lib'

test('E29 «А потом?»: музей → ужин, предлагает один, подтверждает другой, в календаре два события', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const [museum, dinner] = await addIdeas(v, ['Музей космонавтики', 'Ужин в грузинском ресторане', 'Прогулка по набережной'])
  await api(v, 'POST', `/ideas/${dinner}/wish`, { wish: 'very' })
  await api(n, 'POST', `/ideas/${dinner}/wish`, { wish: 'want' })
  const { round } = await api(v, 'POST', `/rounds/with/${museum}`)
  const day = round.days[1]
  await api(v, 'POST', `/rounds/${round.id}/slots`, { slots: [day + ':1', day + ':2'] })
  const m = (await api(n, 'POST', `/rounds/${round.id}/slots`, { slots: [day + ':1', day + ':2'] })).round
  expect(m.state).toBe('planned')
  expect(m.extra_suggest[0].title).toBe('Ужин в грузинском ресторане')

  const vp = await open(await phone(browser), v, 'round/' + round.id)
  const np = await open(await phone(browser, { dark: true }), n, 'round/' + round.id)
  const route = vp.getByTestId('route')
  await expect(route).toContainText('А потом?')
  await shot(vp, 'E29-suggest')
  await route.getByRole('button', { name: /Добавить после: Ужин/ }).click()
  await expect(route).toContainText(/Ждём Настю/)
  // Настя sees it live and answers in the app
  const nr = np.getByTestId('route')
  await expect(nr.getByRole('button', { name: 'Добавим' })).toBeVisible({ timeout: 4000 })
  await shot(np, 'E29-proposed-dark')
  await waitMsg(n, /Вова предлагает после «Музей космонавтики»: .*Ужин/)
  await nr.getByRole('button', { name: 'Добавим' }).click()
  await expect(route.locator('.route__stop[data-state="accepted"]')).toHaveCount(1, { timeout: 4000 })
  await waitMsg(v, /Настя за: .*Ужин/)
  // Настя adds her own in words; Вова declines it in the bot
  await nr.getByLabel('Добавить продолжение').fill('Прогулка по Арбату')
  await nr.getByRole('button', { name: 'Добавить своё продолжение' }).click()
  await waitMsg(v, /Настя предлагает после .*Прогулка по Арбату/)
  await press(v, 'Не в этот раз')
  await expect(route.locator('.route__stop')).toHaveCount(2, { timeout: 4000 }) // main + dinner
  // the meeting was planned at the match; its calendar link now carries both stops
  const plannedMsg = await waitMsg(v, /Мы идём/)
  const link = JSON.stringify(plannedMsg.reply_markup).match(/https?:[^"]+\/cal\/[0-9a-f]{36}\/\d+\.ics/)![0].replace(/^https?:\/\/[^/]+/, BASE)
  const ics = await (await fetch(link)).text()
  expect(ics.match(/BEGIN:VEVENT/g)!.length).toBe(2)
  expect(ics).toContain('SUMMARY:Ужин в грузинском ресторане')
  await vp.reload()
  await expect(vp.locator('.ticket')).toContainText('Потом')
  await shot(vp, 'E29-ticket')
})
