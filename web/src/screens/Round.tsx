import { useEffect, useMemo, useRef, useState } from 'react'
import { motion } from 'framer-motion'
import type { Home, Idea, Round } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, humanError, ApiError } from '../lib/api'
import { go } from '../lib/router'
import { dayLabel, hm, money, dur, relLeft, when, parseDay, ncase } from '../lib/format'
import { haptic, download } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Btn, Cover, Empty, Marks, Sheet, Skel, SubHeader, burst, toast } from '../ui/kit'
import { MonthGrid, type DayMark } from '../ui/Charts'
import { Places } from '../ui/Places'
import { useOnline } from '../lib/live'
import { RouteBlock } from '../ui/Route'
import { openLink } from '../lib/tg'

type R = { round: Round }

export function RoundScreen({ id, home }: { id: number; home: Home | null }) {
  const { data, set, refresh } = useLoad<R>('/rounds/' + id, 5000)
  const here = useOnline(home?.partner?.color)
  const prev = useRef<string>('')
  const r = data?.round
  useEffect(() => {
    if (!r) return
    const key = r.state
    if (prev.current && prev.current !== key && (key === 'confirm' || key === 'planned')) burst()
    prev.current = key
  }, [r?.state])
  if (!r || !home) return <><SubHeader /><Skel h={60} /><Skel h={380} r={36} /></>
  const act = async (action: string, body?: object) => {
    try { const x = await post<R>(`/rounds/${r.id}/${action}`, body); set(x); emit('home'); return x }
    catch (e) { if (e instanceof ApiError && e.status === 409) refresh(); toast(humanError(e)); throw e }
  }
  const partner = home.partner?.name || 'Партнёр'
  let body
  switch (r.state) {
    case 'time': body = <TimeStage r={r} act={act} partner={partner} here={here} />; break
    case 'vote': body = <VoteStage r={r} act={act} partner={partner} home={home} />; break
    case 'confirm': body = <ConfirmStage r={r} act={act} partner={partner} home={home} onPlace={refresh} onRound={set} />; break
    case 'planned': body = <PlannedStage r={r} act={act} home={home} onPlace={refresh} onRound={set} />; break
    case 'done': body = <DoneStage r={r} act={act} />; break
    default: body = <Closed r={r} />
  }
  return <><SubHeader title={titles[r.state]} right={['time', 'vote', 'confirm'].includes(r.state) ? <span className="caption muted">{relLeft(r.expires_at)}</span> : undefined} />{body}</>
}

const titles: Record<string, string> = { time: 'Когда удобно', vote: 'Выбираем', confirm: 'Совпало', planned: 'Встреча', done: 'Встреча', expired: 'Выбор закрыт', cancelled: 'Отменено' }
type Act = (a: string, b?: object) => Promise<R>

// ---------- 1. time ----------
const PARTS: [number, string, string][] = [[0, 'Утро', 'sunrise'], [1, 'День', 'sun'], [2, 'Вечер', 'moon']]

const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
const monday = (d: Date) => { const x = new Date(d.getFullYear(), d.getMonth(), d.getDate()); x.setDate(x.getDate() - (x.getDay() + 6) % 7); return x }
const addDays = (d: Date, n: number) => { const x = new Date(d); x.setDate(x.getDate() + n); return x }
const MONG = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря']
const weekTitle = (m: Date) => { const e = addDays(m, 6); return m.getMonth() === e.getMonth() ? `${m.getDate()}–${e.getDate()} ${MONG[e.getMonth()]}` : `${m.getDate()} ${MONG[m.getMonth()].slice(0, 3)} – ${e.getDate()} ${MONG[e.getMonth()].slice(0, 3)}` }
const PART_END = [13, 18, 23]
const slotWord = (n: number) => n % 10 === 1 && n % 100 !== 11 ? 'слот' : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20) ? 'слота' : 'слотов'

function TimeStage({ r, act, partner, here }: { r: Round; act: Act; partner: string; here?: boolean }) {
  const [sel, setSel] = useState<Set<string>>(new Set(r.my_slots))
  const [editing, setEditing] = useState(!r.sent)
  const today = r.today || ymd(new Date())
  const last = r.horizon || ymd(addDays(parseDay(today), 60))
  const sug = new Set(r.days)
  // a rolling 7-day window, never starting in the past: opens on the suggested days (or the earliest mark)
  const t0 = parseDay(today), lastStart = addDays(parseDay(last), -6)
  const clamp = (d: Date) => d < t0 ? t0 : d > lastStart ? lastStart : d
  // a day picked in the calendar opens its week and is highlighted
  const [focus, setFocus] = useState(() => { try { const d = sessionStorage.getItem('vd-focus-day') || ''; sessionStorage.removeItem('vd-focus-day'); return d >= today ? d : '' } catch { return '' } })
  const [week, setWeek] = useState(() => clamp(monday(parseDay(focus || [...r.my_slots].sort()[0]?.slice(0, 10) || r.days[0] || today))))
  useEffect(() => { if (!focus) return; const t = setTimeout(() => setFocus(''), 2600); return () => clearTimeout(t) }, [focus])
  const [dir, setDir] = useState(0)
  const [picker, setPicker] = useState(false)
  const paint = useRef<null | boolean>(null)
  const toggle = (k: string, on?: boolean) => setSel(s => { const n = new Set(s); const v = on ?? !n.has(k); if (v === n.has(k)) return s; v ? n.add(k) : n.delete(k); haptic.select(); return n })
  const send = async () => { await act('slots', { slots: [...sel] }); setEditing(false) }
  // «любое возможное»: a whole day, one part of the day all week, or everything in the next two weeks
  const setMany = (ks: string[]) => { const free = ks.filter(k => !past(k.slice(0, 10), +k.slice(11)) && !myBusy.has(k)); if (!free.length) return
    const on = !free.every(k => sel.has(k)); haptic.select(); setSel(s => { const n = new Set(s); free.forEach(k => on ? n.add(k) : n.delete(k)); return n }) }
  const anyTime = () => { const ks: string[] = []; for (let d = 0; d < 14; d++) { const day = ymd(addDays(t0, d)); if (day > last) break; PARTS.forEach(([p]) => ks.push(`${day}:${p}`)) }
    const free = ks.filter(k => !past(k.slice(0, 10), +k.slice(11)) && !myBusy.has(k)); setSel(s => new Set([...s, ...free])); haptic.ok()
    toast(`Отметил всё свободное на две недели — ${free.length} ${slotWord(free.length)}${myBusy.size ? ' (занятое по твоему календарю пропустил)' : ''}. Лишнее можно снять`) }
  const noOverlap = r.sent && r.partner_sent
  const goWeek = (m: Date) => { const t = clamp(m); if (+t === +week) return; haptic.select(); setDir(t > week ? 1 : -1); setWeek(t) }
  const perDay = useMemo(() => { const m: Record<string, number> = {}; sel.forEach(k => { const d = k.slice(0, 10); m[d] = (m[d] || 0) + 1 }); return m }, [sel])
  const perWeek = useMemo(() => { const m: Record<string, number> = {}; Object.entries(perDay).forEach(([d, n]) => { const w = ymd(monday(parseDay(d))); m[w] = (m[w] || 0) + n }); return m }, [perDay])
  const nowH = new Date().getHours()
  const past = (d: string, p: number) => d < today || d > last || (d === today && nowH >= PART_END[p])
  // time is agreed in the open: the partner's proposals are visible, one tap takes one of them
  const theirs = useMemo(() => new Set(r.partner_slots || []), [r.partner_slots?.join()])
  const myBusy = useMemo(() => new Set(r.my_busy || []), [r.my_busy?.join()])
  const pBusy = useMemo(() => new Set(r.partner_busy || []), [r.partner_busy?.join()])
  const accept = async (k: string) => { haptic.ok(); await act('slots', { slots: [...new Set([k, ...r.my_slots])] }); setEditing(false) }
  const theirList = [...theirs].sort().filter(k => !past(k.slice(0, 10), +k.slice(11)))
  const Proposals = ({ title }: { title: string }) => theirList.length ? (
    <div className="frame rise" data-testid="partner-time"><div className="frame__in">
      <p className="callout" style={{ margin: '0 0 10px' }}><b>{title}</b></p>
      <div className="col" style={{ gap: 8 }}>
        {theirList.slice(0, 6).map(k => { const L = dayLabel(k.slice(0, 10)); return (
          <button key={k} className="proposal" onClick={() => accept(k)}>
            <span>{L.long}, {L.num} {L.mon} · {PARTS[+k.slice(11)][1].toLowerCase()}</span><b>Подходит</b>
          </button>) })}
      </div>
    </div></div>) : null

  if (!editing) {
    return (
      <div className="col">
        {r.chosen && <ChosenStrip i={r.chosen} />}
        <div className="frame rise"><div className="frame__in" style={{ textAlign: 'center' }}>
          <div className="meet" style={{ margin: '8px auto 16px' }}><i /><i /></div>
          {noOverlap ? <>
            <h2 className="title">Пока не совпало</h2>
            <p className="callout muted">Выбери время {ncase(partner, 'gen')} ниже — или предложи другой день. В календарь встреча попадёт только после «да» от вас обоих.</p>
          </> : <>
            <h2 className="title">Ждём {ncase(partner, 'acc')}</h2>
            {here && <p className="here" style={{ justifyContent: 'center', margin: '6px 0 0' }}>{partner} сейчас в приложении</p>}
            <p className="callout muted">Как только {partner} отметит время, я найду пересечение и пришлю варианты вам обоим.</p>
          </>}
          <Btn kind="soft" sm icon="edit" onClick={() => setEditing(true)}>{noOverlap ? 'Предложить другое' : 'Изменить моё время'}</Btn>
        </div></div>
        {noOverlap && <Proposals title={`${partner} может:`} />}
      </div>
    )
  }
  const days = [...Array(7)].map((_, i) => ymd(addDays(week, i)))
  const fri = (() => { const wd = t0.getDay(); return wd === 0 || wd === 6 ? t0 : addDays(t0, 5 - wd) })()
  const nextMon = addDays(monday(t0), 7)
  const quick = ([['Ближайшие дни', t0], ['Выходные', fri], ['След. неделя', nextMon]] as [string, Date][]).filter(([, d], i, all) => i === 0 || +d !== +all[0][1])
  const shown = new Set(days)
  // marks outside the visible window get a chip that leads to them
  const otherWeeks = Object.entries(perWeek).filter(([w]) => Object.keys(perDay).some(d => ymd(monday(parseDay(d))) === w && !shown.has(d))).sort()
  return (
    <div className="col">
      {r.chosen && <ChosenStrip i={r.chosen} />}
      {r.partner_sent && <Proposals title={`${partner} предлагает — подходит?`} />}
      <p className="callout muted rise" style={{ margin: 0 }}>{r.partner_sent ? <>Или отметь своё — <span className="their-dot" /> это время {ncase(partner, 'gen')}. Совпадёт хоть одно — договорились, потом оба подтвердите.</> : <>Предложи, когда можешь — на любой день. {partner} увидит и выберет подходящее.</>}</p>
      <div className="wknav rise" style={{ ['--d' as any]: 1 }}>
        <button className="wknav__arrow" aria-label="Предыдущая неделя" disabled={+week <= +t0} onClick={() => goWeek(addDays(week, -7))}><Icon n="back" s={16} w={2.4} /></button>
        <button className="wknav__mid" onClick={() => setPicker(true)} aria-label="Выбрать неделю в календаре"><Icon n="cal" s={17} />{weekTitle(week)}{perWeek[ymd(week)] ? <small>· {perWeek[ymd(week)]}</small> : null}</button>
        <button className="wknav__arrow" aria-label="Следующая неделя" disabled={+week >= +lastStart} onClick={() => goWeek(addDays(week, 7))}><Icon n="chev" s={16} w={2.4} /></button>
      </div>
      <div className="wkchips rise" style={{ ['--d' as any]: 1 }}>
        {quick.map(([l, d]) => <button key={l} className="wkchip" data-on={+clamp(d) === +week || undefined} onClick={() => goWeek(d)}>{l}</button>)}
        {otherWeeks.map(([w, n]) => <button key={w} className="wkchip" onClick={() => goWeek(parseDay(w))}>{weekTitle(parseDay(w))}<b>{n}</b></button>)}
        <button className="wkchip" onClick={() => setPicker(true)}><Icon n="cal" s={14} />Другая дата</button>
        <button className="wkchip" data-testid="any-time" onClick={anyTime}>Мне подходит любое</button>
        {sel.size > 0 && <button className="wkchip" onClick={() => { haptic.select(); setSel(new Set()) }}>Сбросить</button>}
      </div>
      <motion.div key={ymd(week)} className="slots slots--week" initial={dir ? { opacity: 0, x: dir * 28 } : false} animate={{ opacity: 1, x: 0 }} transition={{ type: 'spring', stiffness: 420, damping: 36 }}
        onPointerUp={() => { paint.current = null }} onPointerLeave={() => { paint.current = null }}>
        <span />{PARTS.map(([p, l, ic]) => <span key={l} role="button" aria-label={`${l} всю неделю`} className="slots__h row" style={{ justifyContent: 'center', gap: 4 }} onClick={() => setMany(days.map(d => `${d}:${p}`))}><Icon n={ic} s={14} w={2} />{l}</span>)}
        {days.map(d => { const L = dayLabel(d); const off = d < today || d > last; return [
          <div key={d} role="button" aria-label={`${L.long} целиком`} onClick={() => setMany(PARTS.map(([p]) => `${d}:${p}`))} className={'slots__day' + (sug.has(d) ? ' is-sug' : '') + (off ? ' is-off' : '') + (d === focus ? ' is-focus' : '')}><b>{L.wd}</b><span>{L.num} {L.mon}</span></div>,
          ...PARTS.map(([p]) => { const k = `${d}:${p}`; const dis = past(d, p); return (
            <button key={k} data-k={k} className="cell" disabled={dis} aria-pressed={sel.has(k)} data-their={theirs.has(k) || undefined} data-common={(theirs.has(k) && sel.has(k)) || undefined} data-busy={myBusy.has(k) || undefined} data-pbusy={pBusy.has(k) || undefined} aria-label={`${L.long} ${L.num} ${L.mon} ${PARTS[p][1]}${myBusy.has(k) ? ', занято в твоём календаре' : ''}${pBusy.has(k) ? `, ${partner} занят(а)` : ''}`} style={{ ['--c' as any]: 'var(--person-a)' }}
              onPointerDown={e => { if (dis) return; (e.target as HTMLElement).releasePointerCapture?.(e.pointerId); paint.current = !sel.has(k); toggle(k, paint.current) }}
              onPointerEnter={() => { if (paint.current !== null && !dis) toggle(k, paint.current) }}>
              <Icon n="check" s={20} w={2.6} />
            </button>) }),
        ] })}
      </motion.div>
      {(myBusy.size > 0 || pBusy.size > 0) ? <p className="caption" style={{ margin: 0 }}><span className="busy-key" /> занято по твоему календарю{pBusy.size > 0 && <> · <span className="pbusy-key" /> {partner} занят(а)</>}</p>
        : <button className="wkchip" style={{ alignSelf: 'flex-start' }} onClick={() => go('calendar')}><Icon n="cal" s={14} />Подключи свой календарь — занятое отметится само</button>}
      <p className="caption muted" style={{ margin: 0 }}>Утро 9–13 · день 13–18 · вечер 18–23. Можно вести пальцем; нажми на день — отметится целиком, на «Вечер» — все вечера недели. <span style={{ color: '#ff701e' }}>●</span> — предлагаю эти дни.</p>
      <div className="stickybar"><Btn kind="primary" block onClick={send} disabled={sel.size === 0}>{sel.size === 0 ? 'Отметь хотя бы одно' : `Готово · ${sel.size} ${slotWord(sel.size)}`}</Btn></div>
      <Sheet open={picker} onClose={() => setPicker(false)}>
        <WeekPicker today={today} last={last} perDay={perDay} week={week} onPick={d => { goWeek(monday(parseDay(d))); setFocus(d); setPicker(false); toast('Теперь отметь, когда удобно в этот день') }} />
      </Sheet>
    </div>
  )
}

function WeekPicker({ today, last, perDay, week, onPick }: { today: string; last: string; perDay: Record<string, number>; week: Date; onPick: (d: string) => void }) {
  const [month, setMonth] = useState(() => { const t = parseDay(today), w = week < t ? t : week; return new Date(w.getFullYear(), w.getMonth(), 1) })
  const marks = useMemo(() => { const m: Record<string, DayMark> = {}; Object.entries(perDay).forEach(([d, n]) => { m[d] = { picked: n } }); return m }, [perDay])
  const first = parseDay(today), end = parseDay(last)
  const canMove = (n: number) => { const t = new Date(month.getFullYear(), month.getMonth() + n, 1); return t >= new Date(first.getFullYear(), first.getMonth(), 1) && t <= new Date(end.getFullYear(), end.getMonth(), 1) }
  return (<>
    <h2 className="title" style={{ margin: '4px 0 2px' }}>Выбери день</h2>
    <p className="caption muted" style={{ margin: '0 0 10px' }}>Откроется неделя с этим днём. Можно отмечать до {dayLabel(last).num} {dayLabel(last).mon}.</p>
    <MonthGrid month={month} marks={marks} today={today} selected="" last={last} legend={false} onSelect={onPick} onMonth={n => { if (canMove(n)) setMonth(m => new Date(m.getFullYear(), m.getMonth() + n, 1)) }} />
  </>)
}

function ChosenStrip({ i }: { i: Idea }) {
  return (
    <div className="card row rise" style={{ padding: 8, gap: 12 }}>
      <div style={{ width: 64, flex: 'none' }}><Cover idea={i} h={64} /></div>
      <div style={{ minWidth: 0 }}><p className="micro muted" style={{ margin: '0 0 4px' }}>Решили</p><b className="headline">{i.title}</b></div>
    </div>
  )
}

// ---------- 2. vote: swipe deck ----------
function VoteStage({ r, act, partner, home }: { r: Round; act: Act; partner: string; home: Home }) {
  const [top, setTop] = useState(0)
  const n = r.triple.length
  const mismatch = r.voted && r.partner_voted
  const vote = async (id: number) => { haptic.heavy(); await act('vote', { idea_id: id }) }
  if (r.voted && !mismatch) {
    const mine = r.triple.find(i => i.id === r.my_vote)
    return (
      <div className="col">
        <div className="frame rise"><div className="frame__in" style={{ textAlign: 'center' }}>
          <div className="meet" style={{ margin: '8px auto 16px' }}><i /><i /></div>
          <h2 className="title">Твой голос принят</h2>
          <p className="callout muted">Ты выбрал(а) «{mine?.title || 'ни один'}». Голос {ncase(partner, 'gen')} откроется, когда он(а) проголосует — так честнее.</p>
        </div></div>
        <p className="caption muted" style={{ textAlign: 'center', margin: '4px 0 0' }}>Передумал(а)? Можно переголосовать, пока {partner} не ответил(а).</p>
        <Deck r={r} top={top} setTop={setTop} onVote={vote} me={home.me} small />
      </div>
    )
  }
  return (
    <div className="col">
      {mismatch && (
        <div className="card rise" style={{ display: 'grid', gap: 10 }}>
          <b className="headline">Голоса разошлись</b>
          <p className="callout muted" style={{ margin: 0 }}>Ты — «{r.triple.find(i => i.id === r.my_vote)?.title || 'ни один'}», {partner} — «{r.triple.find(i => i.id === r.partner_vote)?.title || 'ни один'}».</p>
          {r.compromise && <Btn kind="accent" block onClick={() => vote(r.compromise!.id).catch(() => {})}>Давай «{r.compromise.title}» — вы оба не против</Btn>}
        </div>
      )}
      {n === 0 ? <Empty title="Нечего предложить" text="Под ваше время в копилке ничего не подошло. Добавьте идей — или возьмите другую тройку." /> :
        <Deck r={r} top={top} setTop={setTop} onVote={vote} me={home.me} />}
      <div className="row">
        <Btn kind="soft" block icon="shuffle" onClick={() => act('reshuffle').then(() => setTop(0)).catch(() => {})}>Другая тройка</Btn>
        <Btn kind="ghost" block onClick={() => vote(-1).catch(() => {})}>Ни один</Btn>
      </div>
    </div>
  )
}

function Deck({ r, top, setTop, onVote, me, small }: { r: Round; top: number; setTop: (n: number) => void; onVote: (id: number) => Promise<void>; me: Home['me']; small?: boolean }) {
  const n = r.triple.length
  const drag = useRef<{ x: number; y: number; id: number } | null>(null)
  const [dx, setDx] = useState(0)
  const next = () => { haptic.select(); setTop((top + 1) % n) }
  const end = async () => {
    const d = dx; drag.current = null; setDx(0)
    if (d > 90) await onVote(r.triple[top].id).catch(() => {})
    else if (d < -90) next()
  }
  return (<>
    <div className="deck" style={small ? { height: 340 } : undefined}>
      {r.triple.map((i, k) => {
        const pos = (k - top + n) % n
        const isTop = pos === 0
        const style = isTop && dx ? { transform: `translateX(${dx}px) rotate(${dx / 18}deg)` } : undefined
        return (
          <div key={i.id} className="dcard" data-pos={pos > 2 ? 'hidden' : isTop ? undefined : pos} data-drag={isTop && drag.current ? '' : undefined} style={{ ...style, zIndex: 10 - pos }}
            onPointerDown={e => { if (!isTop || (e.target as HTMLElement).closest('button')) return; drag.current = { x: e.clientX, y: e.clientY, id: i.id }; (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId) }}
            onPointerMove={e => { if (drag.current) setDx(e.clientX - drag.current.x) }}
            onPointerUp={end} onPointerCancel={() => { drag.current = null; setDx(0) }}>
            {isTop && <><span className="dcard__stamp dcard__stamp--yes" style={{ opacity: Math.max(0, dx / 90) }}>ГОЛОСУЮ</span>
              <span className="dcard__stamp dcard__stamp--no" style={{ opacity: Math.max(0, -dx / 90) }}>ДАЛЬШЕ</span></>}
            <Cover idea={i} big>
              <div className="row">{k === 0 && <span className="pill pill--glass"><Icon n="spark" s={13} w={2.2} />мой фаворит</span>}
                {r.my_vote === i.id && <span className="pill pill--glass"><Icon n="check" s={13} w={2.6} />твой голос</span>}</div>
              <div className="cover__title" style={{ fontSize: 24, lineHeight: '28px' }}>{i.title}</div>
            </Cover>
            <div className="dcard__body">
              <div className="row"><span className="caption muted" style={{ flex: 1 }}>{[i.address, i.price_min ? '≈ ' + money(i.price_min) : '', dur(i.duration_min)].filter(Boolean).join(' · ')}</span><Marks idea={i} me={me} /></div>
              <Btn kind={r.my_vote === i.id ? 'soft' : 'primary'} block icon="check" onClick={() => onVote(i.id)}>{r.my_vote === i.id ? 'Твой голос' : 'Голосую за это'}</Btn>
            </div>
          </div>
        )
      })}
    </div>
    <div className="deckdots">{r.triple.map((i, k) => <i key={i.id} data-on={k === top || undefined} onClick={() => setTop(k)} />)}</div>
  </>)
}

// ---------- 3. confirm ----------
function ConfirmStage({ r, act, partner, home, onPlace, onRound }: { r: Round; act: Act; partner: string; home: Home; onPlace: () => void; onRound: (x: R) => void }) {
  return (
    <div className="col">
      <div className="meet rise" style={{ margin: '4px auto 4px' }}><i /><i /></div>
      <h1 className="display rise" style={{ textAlign: 'center' }}>Совпало!</h1>
      <p className="callout muted rise" style={{ textAlign: 'center', margin: 0 }}>Встреча запланируется, когда подтвердите оба. Молчание не считается согласием.</p>
      <Ticket r={r} />
      {r.chosen && <Places idea={r.chosen} me={home.me} partner={home.partner} compact onChange={onPlace} />}
      <RouteBlock r={r} home={home} onRound={onRound} />
      <div className="row caption" style={{ justifyContent: 'center', gap: 16 }}>
        <span className="row" style={{ gap: 6 }}><span className="dot" style={{ ['--c' as any]: 'var(--success)', opacity: r.confirmed ? 1 : .3 }} />Ты: {r.confirmed ? 'да' : 'ждём'}</span>
        <span className="row" style={{ gap: 6 }}><span className="dot" style={{ ['--c' as any]: 'var(--success)', opacity: r.partner_confirmed ? 1 : .3 }} />{partner}: {r.partner_confirmed ? 'да' : 'ждём'}</span>
      </div>
      {!r.confirmed ? <Btn kind="accent" block shine icon="check" onClick={() => act('confirm').then(() => {}).catch(() => {})}>Подтверждаю</Btn>
        : <div className="pill pill--ok" style={{ alignSelf: 'center', height: 36, padding: '0 14px' }}><Icon n="check" s={16} w={2.6} />Ты подтвердил(а) — ждём {ncase(partner, 'acc')}</div>}
      <Btn kind="soft" block icon="clock" onClick={() => act('other-time').then(() => {}).catch(() => {})}>Другое время</Btn>
      <Btn kind="ghost" block onClick={() => act('cancel').then(() => go('', true)).catch(() => {})}>Отменить выбор</Btn>
    </div>
  )
}

function Ticket({ r }: { r: Round }) {
  const i = r.chosen
  if (!i) return null
  return (
    <div className="ticket rise" style={{ ['--d' as any]: 2 }}>
      <Cover idea={i} big><span /><div className="cover__title" style={{ fontSize: 24, lineHeight: '28px' }}>{i.title}</div></Cover>
      <div className="ticket__cut" />
      <div className="ticket__rows">
        {r.starts_at && <div className="trow"><Icon n="cal" s={20} /><div><b className="headline">{when(r.starts_at, r.ends_at)}</b>
          {r.windows && r.windows.length > 1 && <div className="caption muted">общее время: {r.windows.map(w => `${hm(w.from)}–${hm(w.to)}`).join(', ')}</div>}</div></div>}
        {i.place?.chosen ? <button className="trow" style={{ appearance: 'none', border: 0, background: 'none', padding: 0, textAlign: 'left', color: 'inherit', cursor: 'pointer' }} onClick={() => openLink(i.place!.maps)}>
          <Icon n="pin" s={20} /><span><b className="callout" style={{ fontWeight: 650 }}>{i.place.title}</b>{i.place.address && <span className="caption muted" style={{ display: 'block' }}>{i.place.address}</span>}</span></button>
          : i.address ? <div className="trow"><Icon n="pin" s={20} /><span className="callout">{i.address}</span></div>
          : <div className="trow"><Icon n="pin" s={20} /><span className="callout muted">Место не выбрано — добавьте варианты ниже</span></div>}
        {(r.route || []).filter(s => s.state === 'accepted').map(s => <div key={s.id} className="trow"><Icon n="chev" s={20} /><span className="callout">Потом {hm(s.starts_at)} — <b style={{ fontWeight: 650 }}>{s.idea.title}</b></span></div>)}
        {!!i.price_min && <div className="trow"><Icon n="wallet" s={20} /><span className="callout">≈ {money(i.price_min)} на двоих</span></div>}
        {(i.category === 'food' || i.category === 'culture') && <div className="trow"><Icon n="bell" s={20} /><span className="callout">Заранее: забронировать или купить билеты</span></div>}
      </div>
    </div>
  )
}

// ---------- 4. planned ----------
function PlannedStage({ r, act, home, onPlace, onRound }: { r: Round; act: Act; home: Home; onPlace: () => void; onRound: (x: R) => void }) {
  const past = (r.starts_at || 0) * 1000 < Date.now()
  return (
    <div className="col">
      <Ticket r={r} />
      {!past && r.chosen && <Places idea={r.chosen} me={home.me} partner={home.partner} compact onChange={onPlace} />}
      {!past && <RouteBlock r={r} home={home} onRound={onRound} />}
      {past ? <>
        <h2 className="title" style={{ textAlign: 'center', marginTop: 8 }}>Сходили?</h2>
        <div className="row"><Btn kind="accent" block onClick={() => act('done', { answer: 'yes' }).then(burst).catch(() => {})}>Да!</Btn>
          <Btn kind="soft" block onClick={() => act('done', { answer: 'moved' }).then(() => {}).catch(() => {})}>Перенесли</Btn></div>
        <Btn kind="ghost" block onClick={() => act('done', { answer: 'no' }).then(() => {}).catch(() => {})}>Не получилось</Btn>
      </> : <>
        {r.book && <div className="frame rise" data-testid="tickets"><div className="frame__in col" style={{ gap: 10 }}>
          {r.tickets ? <p className="callout" style={{ margin: 0 }}>✓ {r.tickets === 'me' ? 'Ты взял(а)' : `${home.partner?.name || 'Партнёр'} взял(а)`} {r.chosen?.category === 'food' ? 'стол' : 'билеты'} — всё готово.</p> : <>
            <p className="callout" style={{ margin: 0 }}><b>{r.book.label}</b> — места разбирают, лучше сразу. Отметь, когда возьмёшь, чтобы {home.partner?.name || 'партнёр'} не купил(а) вторые.</p>
            <div className="row" style={{ gap: 8 }}><Btn kind="accent" block icon="link" onClick={() => openLink(r.book!.url)}>{r.book.label}</Btn>
              <Btn kind="soft" block icon="check" onClick={() => act('tickets').then(() => {}).catch(() => {})}>{r.chosen?.category === 'food' ? 'Стол есть' : 'Билеты есть'}</Btn></div>
          </>}
        </div></div>}
        {r.cal?.subscribed
          ? <p className="callout muted" style={{ textAlign: 'center', margin: 0 }}><Icon n="check" s={16} /> Уже в твоём календаре — он подключён</p>
          : <div className="row" style={{ gap: 8 }}>
            <Btn kind="primary" block icon="cal" onClick={() => r.cal?.ics ? openLink(r.cal.ics) : download(`${location.origin}/api/rounds/${r.id}/ics`, 'vdvoem.ics')}>В календарь</Btn>
            {r.cal?.google && <Btn kind="soft" block onClick={() => openLink(r.cal!.google)}>Google</Btn>}
          </div>}
        <p className="caption muted" style={{ textAlign: 'center', margin: 0 }}>{r.cal?.subscribed ? '' : 'Откроется календарь с заполненным событием. '}Напомню за 2 часа. После встречи спрошу, как прошло.</p>
        <div className="row"><Btn kind="soft" block icon="clock" onClick={() => act('other-time').then(() => {}).catch(() => {})}>Перенести</Btn>
          <Btn kind="ghost" block onClick={() => act('cancel').then(() => go('', true)).catch(() => {})}>Отменить</Btn></div>
      </>}
    </div>
  )
}

// ---------- 5. done ----------
function DoneStage({ r, act }: { r: Round; act: Act }) {
  return (
    <div className="col">
      <Ticket r={r} />
      {!r.again ? <>
        <h2 className="title" style={{ textAlign: 'center', marginTop: 8 }}>Повторим как-нибудь?</h2>
        <div className="row"><Btn kind="accent" block icon="heart" onClick={() => act('again', { answer: 'yes' }).then(() => {}).catch(() => {})}>Да, ещё раз</Btn>
          <Btn kind="soft" block onClick={() => act('again', { answer: 'no' }).then(() => {}).catch(() => {})}>Хватит одного</Btn></div>
      </> : <p className="callout muted" style={{ textAlign: 'center' }}>{r.again === 'yes' ? 'Запомнил: хочется повторить.' : 'Запомнил: одного раза хватило.'}</p>}
      <div className="card row" style={{ gap: 12 }}><Icon n="photo" /><span className="callout" style={{ flex: 1 }}>{r.photos ? `${r.photos} фото со встречи` : 'Пришли боту фото — оно будет только у вас двоих.'}</span></div>
      <Btn kind="ghost" block onClick={() => go('', true)}>На главную</Btn>
    </div>
  )
}

function Closed({ r }: { r: Round }) {
  const again = async () => { try { const x = await post<R>('/rounds'); emit('home'); go('round/' + x.round.id, true) } catch (e) { toast(humanError(e)) } }
  return (
    <Empty title={r.state === 'expired' ? 'Выбор закрылся' : 'Выбор отменён'} text={r.state === 'expired' ? 'Двое суток без ответа — не сложилось. Ничего страшного, идеи остались в копилке.' : 'Идеи остались в копилке — можно начать заново.'}>
      <Btn kind="primary" icon="cal" onClick={again}>Начать заново</Btn>
    </Empty>
  )
}
