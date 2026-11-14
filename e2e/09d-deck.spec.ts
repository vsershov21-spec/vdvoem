import { test, expect } from '@playwright/test'
import { people, pair, phone, open, shot, api, addIdeas } from './lib'

// The main screen is the deck: what the partner already marked comes first; «Хочу» on it is a match —
// «Совпало!» — and «Выбрать время» opens the round with that idea at the time step.
test('E43 колода на главной: свайп, совпадение, выбор времени', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const ids = await addIdeas(n, ['Каток в Парке Горького', 'Квиз в баре', 'Музей Темноты'])
  await api(n, 'POST', `/ideas/${ids[2]}/wish`, { wish: 'want' })
  const p = await open(await phone(browser), v)
  const card = p.getByTestId('triage-card')
  await expect(card).toContainText('Музей Темноты') // the partner's marked one first
  await expect(p.getByLabel('Добавить идею')).toBeVisible() // «add» is the plus of the ask bar
  await expect(p.locator('#tab-ai')).toBeVisible() // the assistant: its own tab and the bar on the main screen
  await shot(p, 'E43-deck')
  await p.getByRole('button', { name: 'Хочу', exact: true }).click()
  await expect(p.getByTestId('match-sheet')).toBeVisible()
  await expect(p.getByTestId('match-sheet')).toContainText('Настя тоже хочет')
  await shot(p, 'E43-match')
  await p.getByRole('button', { name: 'Выбрать время' }).click()
  await expect(p).toHaveURL(/\/round\/\d+/)
  const cur = (await api(v, 'GET', '/rounds/current')).round
  expect(cur.chosen?.id).toBe(ids[2])
  // a skip is not a match; the deck moves on
  const q = await open(await phone(browser), v)
  await expect(q.getByTestId('triage-card')).toBeVisible()
  const first = await q.getByTestId('triage-card').innerText()
  await q.getByRole('button', { name: 'Пропущу' }).click()
  await expect(q.getByTestId('match-sheet')).toHaveCount(0)
  await expect(q.getByTestId('triage-card')).toHaveCount(1) // the thrown card has flown off
  // the card that flies away is still in the page for a moment: wait for one card, then look at it
  await expect(q.getByTestId('triage-card')).toHaveCount(1)
  await expect(q.getByTestId('triage-card')).not.toContainText(first.split('\n')[0])
})
