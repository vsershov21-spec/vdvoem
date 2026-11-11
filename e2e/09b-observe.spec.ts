import { test, expect } from '@playwright/test'
import { people, pair, phone, open, api, addIdeas, BASE } from './lib'

// the owner's view: every assistant request is in the journal (how it was read, by what, how long, the buttons),
// a failure too; every opening of the app leaves its stages; all of it survives in the database (admin reads it)
test('E41 журнал помощника и загрузок для админки', async ({ browser }) => {
  const [v, n] = people('Вова', 'Настя')
  await pair(v, n)
  await addIdeas(v, ['Джазовый вечер', 'Кино', 'Баня'])
  await api(v, 'POST', '/assistant', { text: 'куда сходить в субботу' })
  await api(v, 'POST', '/assistant', { text: 'привет' })
  const p = await open(await phone(browser), v) // an opening of the app: html → tg → boot → enter → data
  await expect(p.locator('.pnav')).toBeVisible({ timeout: 15000 })
  await p.waitForTimeout(800)

  expect((await fetch(BASE + '/agent/assistant', { headers: { 'X-Forwarded-For': '1.2.3.4' } })).status).toBe(404)
  const j = await (await fetch(BASE + '/agent/assistant?days=1')).json()
  const texts = j.rows.map((r: any) => r.text)
  expect(texts).toContain('куда сходить в субботу')
  expect(texts).toContain('привет') // a chat is in the journal too (not in training)
  const sat = j.rows.find((r: any) => r.text === 'куда сходить в субботу')
  expect(sat.intent).toContain('"i":"find"')
  expect(sat.src).not.toBe('')
  expect(sat.ms).toBeGreaterThan(0)
  expect(j.summary.requests).toBeGreaterThanOrEqual(2)

  const b = await (await fetch(BASE + '/agent/boots?days=1')).json()
  const o = b.opens.find((x: any) => x.stages && 'data' in x.stages)
  expect(o, JSON.stringify(b.opens.slice(0, 3))).toBeTruthy()
  expect(Object.keys(o.stages)).toEqual(expect.arrayContaining(['html', 'boot', 'data']))
  expect(b.routes.length).toBeGreaterThan(0)
})
