import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { useLoad, emit } from '../lib/data'
import { post, humanError } from '../lib/api'
import { go } from '../lib/router'
import { haptic, shareLink } from '../lib/tg'
import { CATS, money, when as whenText, past } from '../lib/format'
import type { Idea } from '../lib/types'
import type { HomeV2, InboxRow } from './types'
import { Sheet, Pic, Empty, say } from './ui'
import { particles, still } from './particles'

// The home screen does not scroll. One «Ждёт тебя» row (the rest open as a sheet), the swipe card on all the
// free space, and two tiles at the bottom: the nearest meeting and the chores of today.
export function Home({ home, onAddIdea }: { home: HomeV2 | null; onAddIdea: () => void }) {
  const [all, setAll] = useState(false)
  const [plus, setPlus] = useState(false)
  if (!home) return <div className="scr fix" />
  if (!home.paired) return <Solo home={home} onAddIdea={onAddIdea} />
  const box = (home.inbox || []).filter(r => r.role !== 'waiting')
  const m = home.next || home.round
  const ch = home.chore_summary
  const d = new Date()
  return (
    <div className="scr fix" data-testid="v2-home">
      <div className="row">
        <div><h2>Привет, {home.me.name}</h2><p className="m">{d.toLocaleDateString('ru', { weekday: 'short', day: 'numeric', month: 'long' })}</p></div>
        <span className="sp" />
        <button className="b s" aria-label="Добавить идею или дело" onClick={() => setPlus(true)}>＋</button>
        <button className="b s" aria-label="Профиль" onClick={() => go('us')}>{home.me.name[0]}{home.partner?.name[0]}</button>
      </div>
      {box.length > 0 && <>
        <div className="lbl">Ждёт тебя · {box.length}{box.length > 1 && <button className="back" onClick={() => setAll(true)}>все {box.length}</button>}</div>
        <InboxLine row={box[0]} />
      </>}
      <Deck partner={home.partner?.name || 'партнёр'} onAddIdea={onAddIdea} />
      <div className="tiles">
        {m ? <button className="in" data-testid="tile-meet" onClick={() => go('meet/' + m.id)}><span><b>{m.chosen?.title || 'Встреча'}</b><small>{m.state === 'planned' && m.starts_at ? whenText(m.starts_at) : 'выбираем время'}</small></span></button>
          : <button className="in" data-testid="tile-meet" onClick={() => go('plans')}><span><b>Встреч нет</b><small>найти общее время</small></span></button>}
        <button className="in" data-testid="tile-chores" onClick={() => go('chores')}><span><b>Дела{ch && ch.today + ch.overdue > 0 ? ' · ' + (ch.today + ch.overdue) : ''}</b>
          <small>{!ch || ch.today + ch.overdue === 0 ? 'на сегодня ничего' : <>{ch.today} на сегодня{ch.overdue > 0 && <>, <em className="red" style={{ fontStyle: 'normal' }}>{ch.overdue} просрочено</em></>}</>}</small></span></button>
      </div>
      <Sheet open={all} onClose={() => setAll(false)} testid="inbox-sheet"><h3>Ждёт тебя</h3>{box.map(r => <InboxLine key={r.id} row={r} />)}</Sheet>
      <Sheet open={plus} onClose={() => setPlus(false)} testid="plus-sheet">
        <h3>Что добавить?</h3>
        <button className="in" onClick={() => { setPlus(false); onAddIdea() }}><i>♡</i><span><b>Идею</b><small>куда сходить: ссылка или название</small></span>›</button>
        <button className="in" onClick={() => { setPlus(false); go('chores') }}><i>☑</i><span><b>Дело</b><small>что сделать, кто и к какому сроку</small></span>›</button>
      </Sheet>
    </div>)
}

// ---------- «Ждёт тебя»: the answer is given in the row itself ----------
const ICONS: Record<string, string> = { time: '📅', stop: '📍', chore: '☑', cancel: '✕', note: '⚙' }
const LABEL: Record<string, Record<string, string>> = {
  time: { yes: 'Да', no: 'Не могу', other: 'Другое время' }, stop: { yes: 'Да', no: 'Нет', other: 'Другое место' },
  chore: { yes: 'Беру', no: 'Не могу', self: 'Беру сам', any: 'Кто угодно' }, cancel: { ok: 'Понятно' }, note: { ok: 'Понятно' },
}
const COMMON: Record<string, string> = { remind: 'Напомнить', withdraw: 'Отозвать' }
const DONE: Record<string, string> = { yes: 'Ответ отправлен', no: 'Ответ отправлен', ok: '', remind: 'Напоминание ушло', withdraw: 'Предложение отозвано', self: 'Дело твоё', any: 'Дело общее' }

export function InboxLine({ row }: { row: InboxRow }) {
  const [busy, setBusy] = useState(false)
  const where = row.kind === 'chore' ? 'chores' : row.kind === 'note' ? 'settings' : 'meet/' + row.ref_id
  const act = async (a: string) => {
    if (a === 'other') { if (row.role === 'declined') post(`/proposals/${row.id}/seen`).catch(() => {}); go(where); return }
    setBusy(true)
    try {
      if (a === 'yes' || a === 'no' || a === 'ok') await post(`/proposals/${row.id}/answer`, { yes: a !== 'no' })
      else if (a === 'self' || a === 'any') await post(`/chores/${row.ref_id}/${a}`)
      else await post(`/proposals/${row.id}/${a}`)
      haptic.ok(); if (DONE[a]) say(DONE[a]); emit('home')
    } catch (e) { say(humanError(e)) } finally { setBusy(false) }
  }
  return (
    <div className={'q' + (row.hot ? ' hot' : '')} data-testid="inbox-row">
      <button className="qt" onClick={() => go(where)}><i>{ICONS[row.kind]}</i><span><b>{row.title}</b><small>{row.sub}</small></span></button>
      {row.actions.length > 0 && <div className="qb">{row.actions.map((a, k) =>
        <button key={a} className={'b s' + (k === 0 ? ' a' : '')} disabled={busy} onClick={() => act(a)}>{LABEL[row.kind]?.[a] || COMMON[a] || a}</button>)}</div>}
    </div>)
}

// ---------- the deck ----------
type Src = 'ours' | 'afisha'
let lastSrc: Src | '' = ''

function Deck({ partner, onAddIdea }: { partner: string; onAddIdea: () => void }) {
  const { data, refresh } = useLoad<{ ideas: Idea[] }>('/triage')
  const [gone, setGone] = useState<number[]>([])
  const [src, setSrcRaw] = useState<Src | ''>(lastSrc)
  const [match, setMatch] = useState<Idea | null>(null)
  const [settle, setSettle] = useState(false)
  const card = useRef<HTMLDivElement>(null)
  const dragged = useRef(false) // a drag must not count as a tap on the picture
  const drag = useRef<{ x: number; y: number; dx: number; dy: number; v: number; lx: number; lt: number; piv: number; moved: boolean } | null>(null)
  const list = (data?.ideas || []).filter(i => !gone.includes(i.id))
  const ours = list.filter(i => i.ours), afisha = list.filter(i => !i.ours)
  const cur: Src = src || (ours.length || !afisha.length ? 'ours' : 'afisha')
  const pile = cur === 'ours' ? ours : afisha
  const i = pile[0]
  const setSrc = (s: Src) => { lastSrc = s; setSrcRaw(s); setSettle(false) }

  const mark = async (idea: Idea, yes: boolean) => {
    setGone(g => [...g, idea.id]); setSettle(true)
    try {
      const r = await post<{ idea?: Idea }>(`/ideas/${idea.id}/wish`, { wish: yes ? 'want' : 'skip' })
      const pw = r.idea?.partner_wish
      if (yes && (pw === 'want' || pw === 'very')) { haptic.ok(); setMatch(r.idea!) }
      emit('home')
    } catch (e) { say(humanError(e)); setGone(g => g.filter(x => x !== idea.id)); refresh() }
  }
  const setP = (dx: number) => {
    const w = card.current?.parentElement; if (!w) return
    const p = Math.min(Math.abs(dx) / 110, 1)
    w.style.setProperty('--p', String(p)); w.style.setProperty('--y', String(dx > 0 ? p : 0)); w.style.setProperty('--n', String(dx < 0 ? p : 0))
  }
  const fling = (yes: boolean, dy = 0, piv = 1, v = 0) => {
    const c = card.current
    if (!i) return
    if (!c || still()) { mark(i, yes); return }
    const dur = Math.max(.2, Math.min(.34, .34 - v * .08))
    c.parentElement!.classList.add('go'); setP(yes ? 200 : -200)
    c.style.transition = `transform ${dur}s cubic-bezier(.3,0,.7,1),opacity ${dur}s ease-in`
    c.style.transform = `translate(${yes ? 440 : -440}px,${dy + (yes ? -30 : 50)}px) rotate(${(yes ? 20 : -20) * piv}deg)`
    c.style.opacity = '.2'
    haptic.soft()
    if (yes) burst(c)
    setTimeout(() => mark(i, yes), dur * 1000)
  }
  const down = (e: React.PointerEvent) => {
    if ((e.target as HTMLElement).closest('.acts,.gb') || !card.current) return
    const r = card.current.getBoundingClientRect()
    drag.current = { x: e.clientX, y: e.clientY, dx: 0, dy: 0, v: 0, lx: e.clientX, lt: performance.now(), piv: e.clientY - r.top > r.height * .55 ? -1 : 1, moved: false }
    card.current.style.transition = 'none'
  }
  const move = (e: React.PointerEvent) => {
    const d = drag.current, c = card.current; if (!d || !c) return
    const now = performance.now()
    d.dx = e.clientX - d.x; d.dy = e.clientY - d.y
    if (now > d.lt) { d.v = .7 * d.v + .3 * (e.clientX - d.lx) / (now - d.lt); d.lx = e.clientX; d.lt = now }
    if (Math.abs(d.dx) > 6) { d.moved = true; c.style.transform = `translate(${d.dx}px,${d.dy * .22}px) rotate(${d.dx / 20 * d.piv}deg)`; setP(d.dx) }
  }
  const up = () => {
    const d = drag.current, c = card.current; drag.current = null
    if (!d || !c) return
    if (Math.abs(d.dx) > 48 || (Math.abs(d.v) > .35 && Math.abs(d.dx) > 14 && Math.sign(d.v) === Math.sign(d.dx))) fling(d.dx > 0, d.dy * .22, d.piv, Math.abs(d.v))
    else { c.style.transition = 'transform .6s var(--spring2)'; c.style.transform = ''; c.parentElement!.classList.add('back'); setP(0); setTimeout(() => c.parentElement?.classList.remove('back'), 400) }
    if (d.moved) { dragged.current = true; setTimeout(() => { dragged.current = false }, 0) }
  }
  const open = (id: number) => { if (!dragged.current) go('idea/' + id) }

  return (<>
    <div className="dtabs" role="tablist">
      {([['ours', 'Наши', ours.length], ['afisha', 'Афиша', afisha.length]] as [Src, string, number][]).map(([k, l, n]) =>
        <button key={k} role="tab" aria-selected={cur === k} className={cur === k ? 'on' : ''} onClick={() => setSrc(k)}>{l} <i>{n}</i></button>)}
      <span className="sp" /><button className="back" onClick={() => go('ideas')}>Все идеи</button>
    </div>
    {!data ? <div className="card swipe0" />
      : i ? <div className="deckwrap" key={i.id}>
        {pile[2] && <Pic idea={pile[2]} className={'under u2' + (settle ? ' rise' : '')} />}
        {pile[1] && <Pic idea={pile[1]} className={'under' + (settle ? ' rise' : '')} />}
        <div className={'swipe ' + (settle ? 'settle' : 'enter')} ref={card} data-testid="deck-card" onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerLeave={up} onPointerCancel={up}>
          <Pic idea={i} role="button" tabIndex={0} aria-label={i.title} onClick={() => open(i.id)} />
          <i className="wash" /><em className="stamp y">Хочу</em><em className="stamp n">Не моё</em>
          <div className="gl">
            {i.partner_marked && <small className="who2">{partner} {past(partner, 'отметил')}</small>}
            <b className="ttl">{i.title}</b>
            <p>{[CATS[i.category]?.name, i.starts_at ? whenText(i.starts_at) : '', i.price_min ? money(i.price_min) : ''].filter(Boolean).join(' · ')}</p>
            {(i.address || i.info?.about || i.why) && <p className="dim">{[i.address, i.info?.about || i.why].filter(Boolean).join(' · ').slice(0, 110)}</p>}
            <div className="row">
              <button className="gb" onClick={() => go('idea/' + i.id)}>Подробнее</button><span className="sp" />
              <span className="acts"><button aria-label="Не моё" data-testid="deck-skip" onClick={() => fling(false)}>✕</button><button className="y" aria-label="Хочу" data-testid="deck-want" onClick={() => fling(true)}>♥</button></span>
            </div>
          </div>
        </div>
      </div>
        : <div className="card swipe0" data-testid="deck-empty" style={{ alignItems: 'center', justifyContent: 'center', textAlign: 'center', padding: '18px 14px' } as CSSProperties}>
          <Empty title={cur === 'ours' ? (data.ideas.some(x => x.ours) || gone.length ? 'Наши разобраны' : 'Своих идей пока нет') : 'Афиша разобрана'}>
            {(cur === 'ours' ? afisha.length : ours.length) > 0
              ? <button className="b p w" onClick={() => setSrc(cur === 'ours' ? 'afisha' : 'ours')}>{cur === 'ours' ? 'Смотреть афишу' : 'К нашим идеям'} · {cur === 'ours' ? afisha.length : ours.length}</button>
              : <button className="b p w" onClick={onAddIdea}>Добавить идею</button>}
          </Empty>
        </div>}
    <Sheet open={!!match} onClose={() => setMatch(null)} testid="match-sheet">
      {match && <Match idea={match} partner={partner} onClose={() => setMatch(null)} />}
    </Sheet>
  </>)
}

function Match({ idea, partner, onClose }: { idea: Idea; partner: string; onClose: () => void }) {
  const cv = useRef<HTMLCanvasElement>(null)
  useEffect(() => { if (cv.current) particles(cv.current, 'heart') }, [])
  return (<>
    <canvas id="heart" ref={cv} width={440} height={340} aria-hidden="true" />
    <h2 className="match-t" style={{ textAlign: 'center' }}>Совпало</h2>
    <p style={{ textAlign: 'center' }}>{partner} тоже хочет: <b>{idea.title}</b></p>
    <button className="b p w" onClick={() => { onClose(); go('idea/' + idea.id) }}>Идём</button>
    <button className="b w" onClick={onClose}>Смотреть дальше</button>
  </>)
}

// hearts fly out of the ♥ button; the layer sits above the screens, so the next card does not cut them
function burst(card: HTMLElement) {
  const root = card.closest('.v2') as HTMLElement | null, b = card.querySelector('.acts .y')
  if (!root || !b) return
  let fx = root.querySelector('#fx') as HTMLElement | null
  if (!fx) { fx = document.createElement('div'); fx.id = 'fx'; root.appendChild(fx) }
  const r = root.getBoundingClientRect(), c = b.getBoundingClientRect(), x = c.left - r.left + c.width / 2, y = c.top - r.top + c.height / 2
  for (let k = 0; k < 12; k++) {
    const el = document.createElement('i'); el.textContent = '♥'; el.style.cssText = `left:${x}px;top:${y}px;font-size:${12 + Math.random() * 12}px`; fx.appendChild(el)
    const a = -Math.PI / 2 + (Math.random() - .5) * 1.9, d = 60 + Math.random() * 90, X = Math.cos(a) * d, Y = Math.sin(a) * d
    el.animate([{ transform: 'translate(-50%,-50%) scale(0)', opacity: 1 }, { transform: `translate(calc(-50% + ${X * .6}px),calc(-50% + ${Y * .6}px)) scale(1.3)`, opacity: 1, offset: .45 }, { transform: `translate(calc(-50% + ${X}px),calc(-50% + ${Y - 18}px)) scale(.2)`, opacity: 0 }],
      { duration: 560 + Math.random() * 260, delay: k * 16, easing: 'cubic-bezier(.16,1,.3,1)', fill: 'both' }).onfinish = () => el.remove()
  }
}

// ---------- before there is a partner ----------
function Solo({ home, onAddIdea }: { home: HomeV2; onAddIdea: () => void }) {
  const [busy, setBusy] = useState(false)
  const invited = !!home.invite_until
  const text = `${home.me.name} зовёт тебя во «Вдвоём»: вместе выбирать, куда сходить, и делить дела`
  const invite = async () => {
    setBusy(true)
    try { const { link } = await post<{ link: string }>('/invite'); shareLink(link, text); say('Ссылка готова: отправь её партнёру'); emit('home') }
    catch (e) { say(humanError(e)) } finally { setBusy(false) }
  }
  return (
    <div className="scr" data-testid="v2-solo">
      <div><h2>Привет, {home.me.name}</h2><p className="m">Партнёра пока нет</p></div>
      <div className="card" style={{ background: 'linear-gradient(150deg,#ef6537,#31408f)', color: '#fff', gap: 10, padding: 16 }}>
        <h2 style={{ color: '#fff' }}>{invited ? 'Ждём партнёра' : 'Позови партнёра'}</h2>
        <p style={{ fontSize: 13.5 }}>{invited ? 'Ссылка живёт 3 дня и сработает один раз.' : 'Вдвоём приложение сводит ваши «хочу», помогает договориться о времени и делит дела.'}</p>
        <div style={{ background: 'rgba(255,255,255,.16)', borderRadius: 12, padding: '9px 10px', fontSize: 13 }}>Партнёр получит: «{text}»</div>
        <div className="row"><button className="b" style={{ background: '#fff', color: '#141414', boxShadow: 'none' }} disabled={busy} data-testid="invite" onClick={invite}>{invited ? 'Отправить ещё раз' : 'Пригласить'}</button></div>
      </div>
      <div className="lbl">Пока ждёшь</div>
      <div className="row"><button className="b p" style={{ flex: 1 }} onClick={onAddIdea}>＋ Идея</button><button className="b" style={{ flex: 1 }} onClick={() => go('chores')}>＋ Дело</button></div>
      <button className="in" onClick={() => go('ideas')}><i>♡</i><span><b>Отметь, что нравится в афише</b><small>отметки останутся, когда партнёр придёт</small></span>›</button>
    </div>)
}
