// «А потом?» — the meeting as a route: the main idea, then up to two next stops. One partner proposes,
// the other confirms; times follow the main one (the server counts them).
import { useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import type { Home, Idea, Round, RouteStop } from '../lib/types'
import { post, del, humanError, ApiError } from '../lib/api'
import { emit } from '../lib/data'
import { go } from '../lib/router'
import { hm } from '../lib/format'
import { haptic } from '../lib/tg'
import { ncase } from '../lib/format'
import { Icon } from './icons'
import { Cover, toast } from './kit'

type R = { round: Round }
const errs: Record<string, string> = { extra_limit: 'В маршруте уже два продолжения.', extra_state: 'Сначала договоритесь о главном.', extra_own: 'Это твоё предложение — ждём партнёра.' }
const fail = (e: unknown) => toast(e instanceof ApiError && errs[e.code] || humanError(e))

function quick(r: Round): [string, string][] {
  const c = r.chosen
  if (!c) return []
  const near = ' рядом с «' + (c.place?.title || c.title) + '»'
  const late = (r.starts_at || 0) && new Date((r.starts_at || 0) * 1000).getHours() >= 17
  if (c.category === 'food') return [['🚶 Прогуляться', 'Прогулка' + near], ['🍨 Десерт', 'Десерт и кофе' + near], ['🍷 Бар', 'Бар' + near]]
  return late
    ? [['🍽 Поужинать', 'Поужинать' + near], ['🍷 Бар', 'Бар' + near], ['☕ Кофе и десерт', 'Кофе и десерт' + near]]
    : [['🍽 Пообедать', 'Пообедать' + near], ['☕ Кофе', 'Кофе' + near], ['🚶 Прогуляться', 'Прогулка' + near]]
}

export function RouteBlock({ r, home, onRound }: { r: Round; home: Home; onRound: (x: R) => void }) {
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(0)
  const partner = home.partner?.name || 'партнёр'
  const route = r.route || []
  const sug = r.extra_suggest || []
  const run = async (key: number, f: () => Promise<R>) => {
    setBusy(key); haptic.tap()
    try { onRound(await f()); emit('home') } catch (e) { fail(e) } finally { setBusy(0) }
  }
  const add = (i: Idea) => run(i.id, () => post<R>(`/route/${r.id}`, { idea_id: i.id }))
  const addText = () => { const t = text.trim(); if (!t) return; setText(''); run(-1, () => post<R>(`/route/${r.id}`, { text: t })) }
  const answer = (s: RouteStop, ok: boolean) => run(s.id, () => post<R>(`/extras/${s.id}/${ok ? 'accept' : 'decline'}`))
  const remove = (s: RouteStop) => run(s.id, () => del<R>(`/extras/${s.id}`))
  if (!r.chosen || !r.starts_at) return null
  return (
    <div className="card route rise" data-testid="route">
      <div className="route__head"><b className="headline">А потом?</b>
        <span className="caption muted">{route.length ? 'Маршрут встречи' : 'Куда после? Поесть рядом — в одно касание'}</span></div>
      <ol className="route__line">
        <li className="route__stop is-main"><span className="route__t">{hm(r.starts_at)}</span><span className="route__dot" /><span className="route__name">{r.chosen.title}</span></li>
        <AnimatePresence initial={false}>
          {route.map(s => (
            <motion.li key={s.id} className="route__stop" data-state={s.state} layout
              initial={{ opacity: 0, y: -8, scale: 0.98 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, height: 0, marginTop: 0 }}
              transition={{ type: 'spring', stiffness: 400, damping: 30 }}>
              <span className="route__t">{hm(s.starts_at)}</span><span className="route__dot" />
              <span className="route__body">
                <button className="route__name" onClick={() => go('idea/' + s.idea.id)}>{s.idea.title}</button>
                <span className="caption muted">{hm(s.starts_at)}–{hm(s.ends_at)}{s.tight ? ' · впритык к концу вашего времени' : ''}</span>
                {s.state === 'proposed' && (s.mine
                  ? <span className="route__wait">Ждём {ncase(partner, 'acc')} <button className="link" onClick={() => remove(s)} disabled={busy === s.id}>убрать</button></span>
                  : <span className="route__ask">
                      <button className="btn btn--accent btn--sm" onClick={() => answer(s, true)} disabled={busy === s.id}><Icon n="check" s={15} w={2.6} />Добавим</button>
                      <button className="btn btn--ghost btn--sm" onClick={() => answer(s, false)} disabled={busy === s.id}>Не в этот раз</button>
                    </span>)}
              </span>
              {s.state === 'accepted' && <button className="iconbtn route__x" aria-label={'Убрать ' + s.idea.title} onClick={() => remove(s)}><Icon n="x" s={14} /></button>}
            </motion.li>
          ))}
        </AnimatePresence>
      </ol>
      {route.length < 2 && <>
        {/* the obvious next step for this kind of evening — one tap, the assistant finds the places near */}
        <div className="wkchips" aria-label="Быстро добавить">
          {quick(r).map(([l, t]) => <button key={l} className="wkchip" disabled={busy === -1} onClick={() => run(-1, () => post<R>(`/route/${r.id}`, { text: t }))}>{l}</button>)}
        </div>
        {sug.length > 0 && <div className="route__sug">
          {sug.map(i => (
            <button key={i.id} className="route__card" onClick={() => add(i)} disabled={busy === i.id} aria-label={'Добавить после: ' + i.title}>
              <Cover idea={i} h={64}><span /><span className="route__plus"><Icon n="plus" s={14} w={2.6} /></span></Cover>
              <span className="route__ctitle">{i.title}</span>
            </button>
          ))}
        </div>}
        <form className="places__add" onSubmit={e => { e.preventDefault(); addText() }}>
          <input className="input" placeholder="Своё: «поесть рядом», ссылка…" value={text} maxLength={200} onChange={e => setText(e.target.value)} aria-label="Добавить продолжение" />
          <button className="places__send" type="submit" disabled={!text.trim() || busy === -1} aria-label="Добавить своё продолжение"><Icon n="plus" s={18} w={2.6} /></button>
        </form>
      </>}
    </div>
  )
}
