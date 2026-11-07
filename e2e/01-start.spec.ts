import { test, expect } from '@playwright/test'
import { people, say, waitMsg, pair, open, phone, shot, tab, api, chat } from './lib'

test('E01 знакомство через бота: приглашение работает один раз', async () => {
  const [v, n] = people('Вова', 'Настя')
  const code = await pair(v, n)
  await waitMsg(n, /Вы с Вовой теперь вдвоём/)
  const [x] = people('Третий', 'Нет')
  await say(x, '/start ' + code)
  await waitMsg(x, /уже использована или устарела|уже двое/)
  await say(v, '/invite')
  await waitMsg(v, /Вы уже вдвоём/)
  await say(v, '/help')
  await waitMsg(v, /Как это работает/)
})

test('E02 один в приложении: приглашение, идеи копятся', async ({ browser }) => {
  const [solo] = people('Соло', '-')
  const ctx = await phone(browser)
  const p = await open(ctx, solo)
  await expect(p.getByTestId('invite')).toBeVisible()
  await expect(p.getByTestId('island')).toContainText('ждём партнёра')
  await shot(p, 'E02-solo-home')
  await p.getByLabel('Добавить идею').click()
  await p.getByPlaceholder(/Ссылка на место/).fill('Бранч на Патриках')
  await p.getByRole('button', { name: 'В копилку' }).click()
  await p.waitForTimeout(1200)
  await tab(p, 'bank')
  await expect(p.locator('.folder', { hasText: 'Еда' })).toBeVisible()
  await shot(p, 'E02-solo-bank')
  // the bot works for the solo user too
  await say(solo, '/invite')
  const m = await waitMsg(solo, /Пригласи партнёра/)
  expect(m.text).toContain('start=inv_')
  expect((await api(solo, 'GET', '/home')).paired).toBe(false)
  expect((await chat(solo)).length).toBeGreaterThan(0)
})
