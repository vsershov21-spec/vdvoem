// «А потом?» — the meeting as a route: the main idea, then up to two next stops. One partner proposes,
// the other confirms; times follow the main one (the server counts them).
import { useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import type { Home, Idea, Round, RouteStop } from '../lib/types'
import { post, del, humanError, ApiError } from '../lib/api'
import { emit, useLoad } from '../lib/data'
import { go } from '../lib/router'
import { hm } from '../lib/format'
import { haptic } from '../lib/tg'
import { ncase } from '../lib/format'
import { Icon } from './icons'
import { Btn, Cover, Sheet, Skel, toast } from './kit'

type R = { round: Round }
const errs: Record<string, string> = { extra_limit: 'В маршруте уже два продолжения.', extra_state: 'Сначала договоритесь о главном.', extra_own: 'Это твоё предложение — ждём партнёра.' }
const fail = (e: unknown) => toast(e instanceof ApiError && errs[e.code] || humanError(e))

// a stop is a slot first («пообедать»), the place is chosen: from our liked places, the scout's, the assistant's
// picks near — or later. Nothing is decided for the couple.
type Kind = { label: string; what: string; text: string; cat: 'food' | 'walk' | 'bar' }
function quick(r: Round): Kind[] {
  const c = r.chosen
  if (!c) return []
  const near = ' рядом с «' + (c.place?.title || c.title) + '»'
  const late = (r.starts_at || 0) && new Date((r.starts_at || 0) * 1000).getHours() >= 17
  const walk: Kind = { label: '🚶 Прогуляться', what: 'Где погулять', text: 'Прогулка' + near, cat: 'walk' }
  const bar: Kind = { label: '🍷 Бар', what: 'Какой бар', text: 'Бар' + near, cat: 'bar' }
  if (c.category === 'food') return [walk, { label: '🍨 Десерт', what: 'Где десерт и кофе', text: 'Десерт и кофе' + near, cat: 'food' }, bar]
  return late
    ? [{ label: '🍽 Поужинать', what: 'Где поужинать', text: 'Поужинать' + near, cat: 'food' }, bar, { label: '☕ Кофе и десерт', what: 'Где кофе и десерт', text: 'Кофе и десерт' + near, cat: 'food' }]
    : [{ label: '🍽 Пообедать', what: 'Где пообедать', text: 'Пообедать' + near, cat: 'food' }, { label: '☕ Кофе', what: 'Где выпить кофе', text: 'Кофе' + near, cat: 'food' }, walk]
}
const fits = (i: Idea, k: Kind) => i.kind === 'leisure' && i.status !== 'archived' && (k.cat === 'walk' ? i.category === 'walk'
  : k.cat === 'bar' ? /бар|паб|винот|коктейл/i.test(i.title + ' ' + (i.info?.about || '')) : i.category === 'food')
const wants = (w?: string) => w === 'want' || w === 'very'
// a stop that is still only a slot («Пообедать рядом с …») with no place taken among its options
const slot = (s: RouteStop) => / рядом с «/.test(s.idea.title) && !s.idea.place?.chosen

export function RouteBlock({ r, home, onRound }: { r: Round; home: Home; onRound: (x: R) => void }) {
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(0)
  const [pick, setPick] = useState<{ k: Kind; replace?: RouteStop } | null>(null)
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
                <span className="caption muted">{hm(s.starts_at)}–{hm(s.ends_at)}{s.tight ? ' · впритык к концу вашего времени' : ''}{slot(s) ? ' · место не выбрано' : ''}</span>
                <button className="link route__change" onClick={() => setPick({ k: kindOf(s, r), replace: s })}>{slot(s) ? 'Выбрать место' : 'Другое место'}</button>
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
          {quick(r).map(k => <button key={k.label} className="wkchip" disabled={busy === -1} onClick={() => setPick({ k })}>{k.label}</button>)}
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
      <StopPicker open={!!pick} k={pick?.k} r={r} replace={pick?.replace} onClose={() => setPick(null)} onRound={onRound} />
    </div>
  )
}

// what an existing stop is, to open its picker with the right places
function kindOf(s: RouteStop, r: Round): Kind {
  const near = ' рядом с «' + (r.chosen?.place?.title || r.chosen?.title || '') + '»'
  const t = s.idea.title
  if (s.idea.category === 'walk' || /прогул/i.test(t)) return { label: '', what: 'Где погулять', text: 'Прогулка' + near, cat: 'walk' }
  if (/(^|\s)бар|паб/i.test(t)) return { label: '', what: 'Какой бар', text: 'Бар' + near, cat: 'bar' }
  const dinner = /ужин/i.test(t), coffee = /кофе|десерт/i.test(t)
  return { label: '', what: dinner ? 'Где поужинать' : coffee ? 'Где кофе и десерт' : 'Где поесть', text: (dinner ? 'Поужинать' : coffee ? 'Кофе и десерт' : 'Пообедать') + near, cat: 'food' }
}

// «Где пообедать после …»: our liked places first, then what the scout found, then the assistant's picks
// near, or your own link — the slot is never filled for the couple without a choice
export function StopPicker({ open, k, r, replace, onClose, onRound }: { open: boolean; k?: Kind; r: Round; replace?: RouteStop; onClose: () => void; onRound: (x: R) => void }) {
  const all = useLoad<{ ideas: Idea[] }>(open ? '/ideas' : null)
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const skip = new Set([r.chosen?.id, ...(r.route || []).map(s => s.idea.id)])
  const list = k ? (all.data?.ideas || []).filter(i => fits(i, k) && !skip.has(i.id) && !/ рядом с «/.test(i.title)) : []
  const both = list.filter(i => wants(i.my_wish) && wants(i.partner_wish))
  const ours = list.filter(i => !both.includes(i) && (i.ours || wants(i.my_wish)))
  const scout = list.filter(i => !both.includes(i) && !ours.includes(i)).slice(0, 8)
  const put = async (body: { idea_id?: number; text?: string }) => {
    if (busy) return
    setBusy(true); haptic.tap()
    try {
      if (replace) await del<R>(`/extras/${replace.id}`)
      onRound(await post<R>(`/route/${r.id}`, body)); emit('home'); haptic.ok(); onClose(); setText('')
    } catch (e) { fail(e) } finally { setBusy(false) }
  }
  const row = (i: Idea) => (
    <button key={i.id} className="stoprow" onClick={() => put({ idea_id: i.id })} disabled={busy} data-testid="stop-option">
      <span className="stoprow__img"><Cover idea={i} h={52}><span /><span /></Cover></span>
      <span className="stoprow__main"><b>{i.title}</b><span>{[i.address, i.meta].filter(Boolean).join(' · ')}</span></span>
      <span className="stoprow__add"><Icon n="plus" s={16} w={2.6} /></span>
    </button>)
  const group = (title: string, items: Idea[]) => items.length ? <div className="col" style={{ gap: 6 }}><div className="stoph">{title}</div>{items.map(row)}</div> : null
  return (
    <Sheet open={open && !!k} onClose={onClose}>
      {k && <div className="col" style={{ gap: 12 }} data-testid="stop-picker">
        <h2 className="title" style={{ margin: 0 }}>{k.what} после «{r.chosen?.title}»?</h2>
        <p className="caption muted" style={{ margin: 0 }}>{replace ? 'Поменяем место — партнёр подтвердит.' : 'Выбери место — партнёр подтвердит. Можно решить и потом.'}</p>
        {!all.data ? <Skel h={120} /> : <>
          {group('Вы оба хотите', both)}
          {group('Ваши места', ours)}
          {group('Нашёл скаут', scout)}
          {!list.length && <p className="callout muted" style={{ margin: 0 }}>Подходящих мест в вашем банке пока нет — пусть помощник подберёт рядом, или добавь своё.</p>}
        </>}
        <Btn kind="soft" block icon="spark" busy={busy} onClick={() => put({ text: k.text })}>Помощник подберёт рядом — выберем потом</Btn>
        <form className="places__add" onSubmit={e => { e.preventDefault(); if (text.trim()) put({ text: text.trim() }) }}>
          <input className="input" placeholder="Своё: ссылка или название места" value={text} maxLength={200} onChange={e => setText(e.target.value)} aria-label="Своё место" />
          <button className="places__send" type="submit" disabled={!text.trim() || busy} aria-label="Добавить своё место"><Icon n="plus" s={18} w={2.6} /></button>
        </form>
      </div>}
    </Sheet>
  )
}

