import { test, expect } from '@playwright/test'
import { people, pair, phone, open, shot, api, addIdeas, tab, say, waitMsg } from './lib'

// Both in the app at once: changes arrive over the live stream in about a second (polling would take 5–30 s).
test('E27 вместе в приложении: изменения партнёра и присутствие — в реальном времени', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(v, ['Ужин в ресторане', 'Кино'])
  const vp = await open(await phone(browser), v)
  const np = await open(await phone(browser, { dark: true }), n)
  // presence: each sees the other is here
  await expect(vp.getByText('Настя сейчас здесь')).toBeVisible({ timeout: 4000 })
  await expect(np.getByText('Вова сейчас здесь')).toBeVisible({ timeout: 4000 })
  await expect(vp.locator('.ava[data-online]')).toHaveCount(1)
  await shot(vp, 'E27-presence')

  // an idea Настя adds in the bot appears in Вова's copilka
  await tab(vp, 'bank')
  await say(n, 'Каток в Парке Горького')
  await waitMsg(n, /Сохранил в копилку/)
  await expect(vp.locator('.icard', { hasText: 'Каток в Парке Горького' })).toBeVisible({ timeout: 2500 })

  // the round: Вова waits, Настя answers in her app — Вова's screen moves on by itself
  const { round } = await api(v, 'POST', '/rounds')
  const k = round.days[1] + ':2'
  await api(v, 'POST', `/rounds/${round.id}/slots`, { slots: [k] })
  await vp.goto(`/app/round/${round.id}`)
  await expect(vp.getByText(/Ждём Настю/)).toBeVisible()
  await expect(vp.getByText('Настя сейчас в приложении')).toBeVisible({ timeout: 4000 })
  await np.goto(`/app/round/${round.id}`)
  await np.locator(`.cell[data-k="${k}"]`).click().catch(async () => { await np.getByRole('button', { name: 'Ближайшие дни' }).click(); await np.locator(`.cell[data-k="${k}"]`).click() })
  await np.getByRole('button', { name: /Готово/ }).click()
  await expect(vp.getByTestId('choose')).toBeVisible({ timeout: 2500 })
  // Настя votes first; when Вова picks the same, her screen jumps to «Совпало!»
  const v1 = (await api(n, 'GET', `/rounds/${round.id}`)).round
  await api(n, 'POST', `/rounds/${round.id}/vote`, { idea_id: v1.triple[0].id })
  await expect(vp.getByTestId('their-pick')).toContainText(v1.triple[0].title) // her pick is visible at once
  await vp.getByTestId('their-pick').getByRole('button', { name: 'Идём' }).click()
  await expect(np.getByText('Совпало!')).toBeVisible({ timeout: 2500 })

  // Настя closes the app: the green dot goes away
  await np.close()
  await vp.goto('/app/')
  await expect(vp.getByText('Настя сейчас здесь')).toHaveCount(0, { timeout: 4000 })
})
