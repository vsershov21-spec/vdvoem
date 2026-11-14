import { test, expect } from '@playwright/test'
import { people, pair, open, phone, shot, api, addIdeas, waitMsg, press } from './lib'

test('E35 «важно»: Настя просит ответить — Вове алерт с кнопками, на главной сверху; «Идём» начинает выбор времени', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const [id] = await addIdeas(n, ['Спектакль «Чайка»'])
  // Настя: from the idea page
  const pn = await open(await phone(browser), n, 'idea/' + id)
  await pn.getByTestId('ask-urgent').getByRole('button').click()
  await pn.getByPlaceholder('Билеты быстро разбирают!').fill('Последние места!')
  await pn.getByRole('button', { name: 'Отправить' }).click()
  await expect(pn.getByTestId('urgent-mine')).toBeVisible()
  await shot(pn, 'E35-asked')
  const m = await waitMsg(v, /Настя просит ответить/)
  expect(m.text).toContain('Последние места!')
  // Вова: on top of home
  const pv = await open(await phone(browser), v)
  await expect(pv.getByTestId('urgent-home')).toContainText('Чайка')
  await shot(pv, 'E35-home')
  // answering in the bot starts the choice of time
  await press(v, 'Идём — выбрать время')
  await waitMsg(v, /Осталось найти время|отметь, когда/i)
  const cur = (await api(v, 'GET', '/rounds/current')).round
  expect(cur.chosen?.id).toBe(id)
  expect((await api(v, 'GET', '/home')).urgent).toHaveLength(0)
  // time: «любое» marks every free slot of two weeks
  await pv.goto('/app/round/' + cur.id)
  await pv.getByTestId('any-time').click()
  await expect(pv.getByRole('button', { name: /Готово · \d+/ })).toBeVisible()
  await shot(pv, 'E35-anytime')
})

test('E36 колода на главной: всё без отметки по одной карточке', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(n, ['Каток в Парке Горького', 'Ужин в грузинском ресторане', 'Музей космонавтики', 'Квиз в баре'])
  const p = await open(await phone(browser), v)
  await expect(p.getByTestId('triage-card')).toBeVisible() // the deck is the main screen
  await shot(p, 'E36-triage')
  await p.getByRole('button', { name: 'Очень хочу' }).click()
  await p.getByRole('button', { name: 'Пропущу' }).click()
  await p.getByRole('button', { name: 'Потом' }).click()
  await p.getByRole('button', { name: 'Хочу', exact: true }).click()
  await expect(p.getByText('Всё разобрали')).toBeVisible()
  const ideas = (await api(v, 'GET', '/ideas')).ideas
  expect(ideas.filter((i: any) => i.my_wish).length).toBe(3)
})
