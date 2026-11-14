import { test, expect } from '@playwright/test'
import { people, pair, phone, open, shot, api, tab, BASE } from './lib'

// the server's clock runs ahead in the suite (other tests move it): dates count from the server's «today»
let base = new Date()
const day = (n: number, hm = '19:00') => { const d = new Date(base.getTime() + n * 86400_000); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}T${hm}` }
const ym = (s: string) => s.slice(0, 7)

// The calendar has two modes: «Наше» — only what we chose; «Афиша» — what is on each day, to take into our bank.
// An exhibition that closes soon is not on «Наше» — it lives in the afisha's «Скоро закончатся».
test('E42 календарь: «Наше» без чужих событий, «Афиша» по дням, «Скоро закончатся» отдельно', async ({ browser }) => {
  test.setTimeout(90_000)
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  const space = (await api(v, 'GET', '/me')).space.id
  base = new Date((await api(v, 'GET', '/calendar')).today + 'T12:00')
  const picks = [
    { title: 'Концерт в Зарядье', category: 'culture', starts: day(4), price_rub: 3000, why: 'живой звук', score: 6, url: 'https://afisha.example/e2e-cal/' + space + '/1', image: '' },
    { title: 'Выставка импрессионистов', category: 'culture', starts: '', ends: day(5, '21:00'), price_rub: 1200, why: 'последние дни', score: 6, url: 'https://afisha.example/e2e-cal/' + space + '/2', image: '' },
  ]
  const r = await (await fetch(BASE + '/agent/picks', { method: 'POST', body: JSON.stringify({ space, picks }) })).json()
  expect(r.added).toBe(2)

  // «Наше»: nothing we did not choose
  for (const m of new Set([ym(day(0)), ym(day(4)), ym(day(5))])) {
    const cal = await api(v, 'GET', '/calendar?month=' + m)
    expect(cal.events.map((e: any) => e.title)).not.toContain('Выставка импрессионистов')
    expect(cal.events.map((e: any) => e.title)).not.toContain('Концерт в Зарядье')
  }
  // «Афиша» of the month: the dated event, not the run without a start
  const am = await api(v, 'GET', '/afisha?sort=date&month=' + ym(day(4)))
  expect(am.ideas.map((i: any) => i.title)).toContain('Концерт в Зарядье')
  expect(am.ideas.map((i: any) => i.title)).not.toContain('Выставка импрессионистов')
  // «Скоро закончатся»: the exhibition only
  const end = await api(v, 'GET', '/afisha?horizon=ending')
  expect(end.ideas.map((i: any) => i.title)).toEqual(['Выставка импрессионистов'])
  expect(end.counts.ending).toBe(1)

  const p = await open(await phone(browser), v)
  await tab(p, 'cal')
  await expect(p.locator('.sched__row', { hasText: 'Выставка' })).toHaveCount(0) // «Планы» hold only our meetings
  await expect(p.getByRole('tab', { name: 'Афиша' })).toHaveCount(0)
  // the afisha by days lives in «Идеи → Афиша»
  await p.goto('/app/ideas/scout')
  await p.getByTestId('afisha-view').click()
  const now = new Date(), [ty, tm] = day(4).split('-').map(Number)
  for (let k = (ty - now.getFullYear()) * 12 + tm - 1 - now.getMonth(); k > 0; k--) await p.getByLabel('Следующий месяц').click()
  const cell = p.getByLabel(day(4).slice(0, 10))
  await expect(cell.locator('.mcal__count.is-ev')).toHaveText('1')
  await cell.click()
  const card = p.locator('.acard', { hasText: 'Концерт в Зарядье' })
  await expect(card).toBeVisible()
  await shot(p, 'E42-afisha-month')
  await card.getByRole('button', { name: 'Хочу' }).click()
  await expect(card.getByRole('button', { name: 'Хочу' })).toHaveAttribute('aria-pressed', 'true')
  await expect.poll(async () => (await api(v, 'GET', '/ideas')).ideas.find((i: any) => i.title === 'Концерт в Зарядье')?.ours).toBe(true)

  // the afisha's own pick
  await p.goto('/app/ideas/scout')
  await p.getByRole('tab', { name: /Скоро закончатся/ }).click()
  await expect(p.locator('.acard')).toHaveCount(1)
  await expect(p.locator('.acard')).toContainText('Выставка импрессионистов')
  await p.context().close()
  void n
})
