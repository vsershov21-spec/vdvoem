import { test, expect } from '@playwright/test'
import { people, pair, phone, open, shot, api, addIdeas, BASE } from './lib'

// the server's clock runs ahead in the suite (other tests move it): dates count from the server's «today»
let base = new Date()
const day = (n: number) => { const d = new Date(base.getTime() + n * 86400_000); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}T19:00` }

// A new couple has no ideas of its own: the deck says so and offers the scout's afisha in one tap;
// with ideas, ours come first and the afisha is the second pile.
test('E44 колода: «Наши» и «Афиша скаута»; своих нет — предлагает афишу', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const space = (await api(v, 'GET', '/me')).space.id
  base = new Date((await api(v, 'GET', '/calendar')).today + 'T12:00')
  const picks = ['Органный концерт при свечах', 'Квиз в баре', 'Каток на ВДНХ'].map((title, k) => ({ title, category: k ? 'other' : 'culture', starts: day(k + 2), price_rub: 1500, why: 'под ваши вкусы', score: 7, url: `https://afisha.example/e44/${space}/${k}`, image: '' }))
  await fetch(BASE + '/agent/picks', { method: 'POST', body: JSON.stringify({ space, picks }) })
  const p = await open(await phone(browser), v)
  await expect(p.getByTestId('deck-empty')).toContainText('Своих идей пока нет')
  await expect(p.getByLabel('Добавить идею').first()).toBeVisible()
  await shot(p, 'E44-empty')
  await p.getByRole('button', { name: /Смотреть афишу скаута · 3/ }).click()
  await expect(p.getByTestId('triage-card')).toBeVisible()
  await expect(p.getByRole('tab', { name: /Афиша скаута/ })).toHaveAttribute('aria-selected', 'true')
  await shot(p, 'E44-afisha')
  // an idea of ours: it opens on «Наши»
  await addIdeas(n, ['Ужин в грузинском ресторане'])
  const q = await open(await phone(browser), v)
  await expect(q.getByRole('tab', { name: /Наши/ })).toHaveAttribute('aria-selected', 'true')
  await expect(q.getByTestId('triage-card')).toContainText('грузинском')
})

// The calendar banner is there only until the phone's calendar is connected; then a header button with status.
// The time step offers to show my busy hours right there, without leaving the choice.
test('E45 календарь телефона: баннер уходит после подключения, занятость — прямо при выборе времени', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const p = await open(await phone(browser), v, 'calendar')
  await expect(p.getByTestId('calsub')).toBeVisible()
  await p.getByTestId('calsub').click()
  await expect(p.getByTestId('calsheet')).toContainText('1. Наши встречи → в телефон')
  await expect(p.getByTestId('calsheet')).toContainText('2. Моя занятость ← из телефона')
  await shot(p, 'E45-sheet')
  // the link itself opens the phone's calendar app: in the test it is only recorded
  await p.evaluate(() => { (window as any).__opened = []; window.open = ((u: string) => { (window as any).__opened.push(u); return null }) as any })
  await p.getByRole('button', { name: /Добавить в (Google Календарь|Календарь iPhone)/ }).click()
  expect(await p.evaluate(() => (window as any).__opened.length)).toBe(1)
  await expect(p.getByTestId('calsheet')).toContainText('ждём телефон')
  await p.keyboard.press('Escape')
  await p.reload()
  await expect(p.getByTestId('calsub')).toHaveCount(0)
  await expect(p.getByTestId('calbtn')).toBeVisible()
  // the time step: «show my busy hours» opens the iCal form in place
  const ids = await addIdeas(v, ['Каток в Парке Горького'])
  await api(v, 'POST', '/rounds', {})
  const r = (await api(v, 'GET', '/rounds/current')).round
  await p.goto('/app/round/' + r.id)
  await expect(p.getByTestId('busy-chip')).toBeVisible()
  await p.getByTestId('busy-chip').click()
  await expect(p.getByTestId('busyform')).toBeVisible()
  await shot(p, 'E45-busy')
  expect(ids.length).toBe(1)
})

// Inner screens have a native back: «‹ <tab you came from>» on the left
test('E46 вложенные экраны: «‹ Идеи» слева ведёт назад', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const [id] = await addIdeas(v, ['Музей космонавтики'])
  const p = await open(await phone(browser), v, 'ideas')
  await p.getByText('Музей космонавтики').first().click()
  await expect(p).toHaveURL(new RegExp('/app/idea/' + id))
  await expect(p.getByRole('button', { name: 'Назад' })).toContainText('Идеи')
  await shot(p, 'E46-back')
  await p.getByRole('button', { name: 'Назад' }).click()
  await expect(p).toHaveURL(/\/app\/ideas(\?|$)/)
})
