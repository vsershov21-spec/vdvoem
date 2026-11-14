import { test, expect } from '@playwright/test'
import { people, pair, phone, open, shot, api, addIdeas } from './lib'

// «А потом?» after the main event: «Пообедать» does not invent a place — it asks where: our liked places
// first, the scout's, the assistant's picks near or your own. A stop's place can be changed later.
test('E47 «А потом?»: обед — выбираем из своих лайков, потом меняем место', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const [planetarium, khachapuri, sakhalin] = await addIdeas(v, ['Планетарий', 'Ресторан «Хачапури и вино»', 'Ресторан «Сахалин»'])
  await api(v, 'POST', `/ideas/${khachapuri}/wish`, { wish: 'very' })
  await api(n, 'POST', `/ideas/${khachapuri}/wish`, { wish: 'want' })
  await api(v, 'POST', `/ideas/${sakhalin}/wish`, { wish: 'want' })
  const { round } = await api(v, 'POST', `/rounds/with/${planetarium}`)
  const day = round.days[1]
  await api(v, 'POST', `/rounds/${round.id}/slots`, { slots: [day + ':1'] })
  expect((await api(n, 'POST', `/rounds/${round.id}/slots`, { slots: [day + ':1'] })).round.state).toBe('planned')

  const p = await open(await phone(browser), v, 'round/' + round.id)
  const route = p.getByTestId('route')
  await route.getByRole('button', { name: /Пообедать|Поужинать/ }).click()
  const picker = p.getByTestId('stop-picker')
  await expect(picker).toContainText('после «Планетарий»')
  await expect(picker).toContainText('Вы оба хотите')
  await expect(picker.getByTestId('stop-option').first()).toContainText('Хачапури и вино')
  await shot(p, 'E47-picker')
  await picker.getByTestId('stop-option').first().click()
  await expect(route).toContainText('Хачапури и вино')
  await expect(route).toContainText(/Ждём Настю/)
  // a different place: the old stop goes, the new one waits for Настя
  await route.getByRole('button', { name: 'Другое место' }).click()
  await p.getByTestId('stop-picker').getByTestId('stop-option').filter({ hasText: 'Сахалин' }).click()
  await expect(route).toContainText('Сахалин')
  await expect(route.locator('.route__stop')).not.toContainText(['Хачапури и вино'])
  await expect(route.locator('.route__stop')).toHaveCount(2) // the main event + the new stop
  const r = (await api(v, 'GET', `/rounds/${round.id}`)).round
  expect(r.route.map((s: any) => s.idea.title)).toEqual(['Ресторан «Сахалин»'])
  await shot(p, 'E47-changed')
})
