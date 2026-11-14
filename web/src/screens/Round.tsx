import { useEffect, useMemo, useRef, useState } from 'react'
import { motion } from 'framer-motion'
import type { Home, Idea, Round } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, upload, humanError, ApiError } from '../lib/api'
import { go } from '../lib/router'
import { dayLabel, hm, money, relLeft, when, parseDay, ncase, past as agreed } from '../lib/format'
import { haptic, download } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Btn, Cover, Empty, Sheet, Skel, SubHeader, burst, toast } from '../ui/kit'
import { MonthGrid, type DayMark } from '../ui/Charts'
import { Places } from '../ui/Places'
import { useOnline } from '../lib/live'
import { RouteBlock, ProposalsBar } from '../ui/Route'
import { openLink } from '../lib/tg'
import { CalSheet } from '../ui/CalLink'

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

const titles: Record<string, string> = { time: 'Когда удобно', vote: 'Куда идём', confirm: 'Совпало', planned: 'Встреча', done: 'Встреча', expired: 'Выбор закрыт', cancelled: 'Отменено' }
type Act = (a: string, b?: object) => Promise<R>

// ---------- 1. time ----------
const PARTS: [number, string, string][] = [[0, 'Утро', 'sunrise'], [1, 'День', 'sun'], [2, 'Вечер', 'moon'], [3, 'Ночь', 'stars']]
const PART_START = [9, 13, 18, 23]

const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
const monday = (d: Date) => { const x = new Date(d.getFullYear(), d.getMonth(), d.getDate()); x.setDate(x.getDate() - (x.getDay() + 6) % 7); return x }
const addDays = (d: Date, n: number) => { const x = new Date(d); x.setDate(x.getDate() + n); return x }
const MONG = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря']
const weekTitle = (m: Date) => { const e = addDays(m, 6); return m.getMonth() === e.getMonth() ? `${m.getDate()}–${e.getDate()} ${MONG[e.getMonth()]}` : `${m.getDate()} ${MONG[m.getMonth()].slice(0, 3)} – ${e.getDate()} ${MONG[e.getMonth()].slice(0, 3)}` }
const PART_END = [13, 18, 23, 27]
// «2026-11-14:1@14:00» → the part of the day and the exact «from» (optional)
const splitKey = (k: string) => { const [b, at] = k.split('@'); return { b, at: at || '' } }
// the times one can narrow a part to: every half hour after its start, until 30 minutes before its end
const timesIn = (p: number) => { const out: string[] = []; for (let m = PART_START[p] * 60 + 30; m <= PART_END[p] * 60 - 30; m += 30) out.push(`${String(Math.floor(m / 60) % 24).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`); return out }
const slotWord = (n: number) => n % 10 === 1 && n % 100 !== 11 ? 'слот' : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20) ? 'слота' : 'слотов'

function TimeStage({ r, act, partner, here }: { r: Round; act: Act; partner: string; here?: boolean }) {
  const [sel, setSel] = useState<Set<string>>(new Set(r.my_slots.map(k => splitKey(k).b)))
  const [at, setAt] = useState<Record<string, string>>(() => Object.fromEntries(r.my_slots.map(splitKey).filter(x => x.at).map(x => [x.b, x.at])))
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
  const send = async () => { await act('slots', { slots: [...sel].map(k => at[k] ? `${k}@${at[k]}` : k) }); setEditing(false) }
  // «любое возможное»: a whole day, one part of the day all week, or everything in the next two weeks
  const setMany = (ks: string[]) => { const free = ks.filter(k => !past(k.slice(0, 10), +k.slice(11)) && !myBusy.has(k)); if (!free.length) return
    const on = !free.every(k => sel.has(k)); haptic.select(); setSel(s => { const n = new Set(s); free.forEach(k => on ? n.add(k) : n.delete(k)); return n }) }
  const anyTime = () => { const ks: string[] = []; for (let d = 0; d < 14; d++) { const day = ymd(addDays(t0, d)); if (day > last) break; PARTS.forEach(([p]) => ks.push(`${day}:${p}`)) }
    const free = ks.filter(k => !past(k.slice(0, 10), +k.slice(11)) && !myBusy.has(k)); setSel(s => new Set([...s, ...free])); haptic.ok()
    toast(`Отметил всё свободное на две недели — ${free.length} ${slotWord(free.length)}${myBusy.size ? ' (занятое по твоему календарю пропустил)' : ''}. Лишнее можно снять`) }
  const noOverlap = r.sent && r.partner_sent
  const goWeek = (m: Date) => { const t = clamp(m); if (+t === +week) return; haptic.select(); setDir(t > week ? 1 : -1); setWeek(t) }
  const weekSel = useMemo(() => { const ds = new Set([...Array(7)].map((_, i) => ymd(addDays(week, i)))); return [...sel].filter(k => ds.has(k.slice(0, 10))).sort() }, [sel, week])
  const perDay = useMemo(() => { const m: Record<string, number> = {}; sel.forEach(k => { const d = k.slice(0, 10); m[d] = (m[d] || 0) + 1 }); return m }, [sel])
  const perWeek = useMemo(() => { const m: Record<string, number> = {}; Object.entries(perDay).forEach(([d, n]) => { const w = ymd(monday(parseDay(d))); m[w] = (m[w] || 0) + n }); return m }, [perDay])
  const nowH = new Date().getHours()
  const past = (d: string, p: number) => d < today || d > last || (d === today && nowH >= PART_END[p])
  // time is agreed in the open: the partner's proposals are visible, one tap takes one of them
  const theirAt = useMemo(() => Object.fromEntries((r.partner_slots || []).map(splitKey).map(x => [x.b, x.at])), [r.partner_slots?.join()])
  const theirs = useMemo(() => new Set(Object.keys(theirAt)), [theirAt])
  const myBusy = useMemo(() => new Set(r.my_busy || []), [r.my_busy?.join()])
  const pBusy = useMemo(() => new Set(r.partner_busy || []), [r.partner_busy?.join()])
  const accept = async (k: string) => { haptic.ok(); const t = theirAt[k]; await act('slots', { slots: [t ? `${k}@${t}` : k, ...r.my_slots.filter(x => splitKey(x).b !== k)] }); setEditing(false) }
  const theirList = [...theirs].sort().filter(k => !past(k.slice(0, 10), +k.slice(11)))
  const Proposals = ({ title }: { title: string }) => theirList.length ? (
    <div className="frame rise" data-testid="partner-time"><div className="frame__in">
      <p className="callout" style={{ margin: '0 0 10px' }}><b>{title}</b></p>
      <div className="col" style={{ gap: 8 }}>
        {theirList.slice(0, 6).map(k => { const L = dayLabel(k.slice(0, 10)); return (
          <button key={k} className="proposal" onClick={() => accept(k)}>
            <span>{L.long}, {L.num} {L.mon} · {theirAt[k] ? 'с ' + theirAt[k] : PARTS[+k.slice(11)][1].toLowerCase()}</span><b>Подходит</b>
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
      <p className="callout muted rise" style={{ margin: 0 }}>{r.partner_sent ? <>Или отметь своё — <span className="their-dot" /> это время {ncase(partner, 'gen')}. Совпадёт хоть одно — договорились.</> : <>Тапни, когда тебе удобно. {partner} увидит и выберет.</>}</p>
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
            <button key={k} data-k={k} className="cell" disabled={dis} aria-pressed={sel.has(k)} data-their={theirs.has(k) || undefined} data-common={(theirs.has(k) && sel.has(k)) || undefined} data-busy={myBusy.has(k) || undefined} data-pbusy={pBusy.has(k) || undefined} aria-label={`${L.long} ${L.num} ${L.mon} ${PARTS[p][1]}${myBusy.has(k) ? ', занято в твоём календаре' : ''}${pBusy.has(k) ? `, у ${ncase(partner, "gen")} занято` : ''}`} style={{ ['--c' as any]: 'var(--person-a)' }}
              onPointerDown={e => { if (dis) return; (e.target as HTMLElement).releasePointerCapture?.(e.pointerId); paint.current = !sel.has(k); toggle(k, paint.current) }}
              onPointerEnter={() => { if (paint.current !== null && !dis) toggle(k, paint.current) }}>
              <Icon n="check" s={20} w={2.6} />
            </button>) }),
        ] })}
      </motion.div>
      {(myBusy.size > 0 || pBusy.size > 0) ? <p className="caption" style={{ margin: 0 }}><span className="busy-key" /> занято по твоему календарю{pBusy.size > 0 && <> · <span className="pbusy-key" /> у {ncase(partner, 'gen')} занято</>}</p>
        : r.my_busy ? <p className="caption muted" style={{ margin: 0 }}><Icon n="check" s={13} /> Твой календарь подключён — на эти две недели занятого нет</p>
        : <BusyChip />}
      {weekSel.length > 0 && <div className="card col rise" data-testid="exact-slots" style={{ gap: 8 }}>
        <span className="caption muted">Точнее, во сколько? Необязательно — по умолчанию весь отрезок</span>
        {weekSel.map(k => { const L = dayLabel(k.slice(0, 10)); const p = +k.slice(11); return (
          <label key={k} className="row" style={{ gap: 8, justifyContent: 'space-between' }}>
            <span className="callout">{L.wd} {L.num} · {PARTS[p][1].toLowerCase()}</span>
            <select className="input" style={{ width: 'auto', minWidth: 132 }} value={at[k] || ''} aria-label={`Во сколько: ${L.long} ${PARTS[p][1]}`}
              onChange={e => { haptic.select(); const v = e.target.value; setAt(a => { const n = { ...a }; if (v) n[k] = v; else delete n[k]; return n }) }}>
              <option value="">{`${String(PART_START[p] % 24).padStart(2, '0')}–${String(PART_END[p] % 24).padStart(2, '0')}, любое`}</option>
              {timesIn(p).map(t => <option key={t} value={t}>с {t}</option>)}
            </select>
          </label>) })}
      </div>}
      <details className="howto"><summary>Утро 9–13 · день 13–18 · вечер 18–23 · ночь 23–03</summary>
        <p>Можно вести пальцем. Нажми на день — отметится целиком, на «Вечер» — все вечера. <span style={{ color: 'var(--accent)' }}>●</span> — дни, которые я предлагаю.</p></details>
      <div className="stickybar"><Btn kind="primary" block onClick={send} disabled={sel.size === 0}>{sel.size === 0 ? 'Выбери хотя бы одно время' : `Готово · ${sel.size} ${slotWord(sel.size)}`}</Btn></div>
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

// ---------- 2. what — from our own matches for the common time, in the open ----------
// No blind vote: the list is what both already want (then the closest), each one's pick is visible at once,
// the same pick by both is the decision.
function VoteStage({ r, act, partner }: { r: Round; act: Act; partner: string; home: Home }) {
  const pick = async (id: number) => { haptic.heavy(); await act('vote', { idea_id: id }).catch(() => {}) }
  const w = r.windows?.[0]
  const whenText = w ? new Date(w.from * 1000).toLocaleString('ru', { weekday: 'long', day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' }) : ''
  const theirs = r.triple.find(i => i.id === r.partner_vote)
  const wants = (x?: string) => x === 'want' || x === 'very'
  return (
    <div className="col" data-testid="choose">
      <div className="rise">
        <h1 className="display" style={{ margin: 0 }}>Вы оба свободны</h1>
        {whenText && <p className="callout muted" style={{ margin: '4px 0 0' }}>{whenText}</p>}
      </div>
      {theirs && r.my_vote !== theirs.id && (
        <div className="props rise" data-testid="their-pick">
          <b className="props__h">{partner} за</b>
          <div className="props__row">
            <span className="props__main"><b>{theirs.title}</b><span>{theirs.meta}</span></span>
            <button className="btn btn--accent btn--sm" onClick={() => pick(theirs.id)}>Идём</button>
          </div>
        </div>
      )}
      {r.triple.length === 0 ? <Empty title="На это время ничего не подошло" text="Добавьте идей или выберите другое время." /> : (
        <div className="col" style={{ gap: 8 }}>
          {r.triple.map(i => {
            const mine = r.my_vote === i.id
            return (
              <div key={i.id} className={'pickrow rise' + (mine ? ' is-mine' : '')} data-testid="choose-option">
                <button className="pickrow__open" onClick={() => go('idea/' + i.id)} aria-label={'Открыть: ' + i.title}>
                  <span className="pickrow__img"><Cover idea={i} h={64}><span /><span /></Cover></span>
                  <span className="pickrow__main"><b>{i.title}</b><span>{i.meta}</span>
                    {wants(i.my_wish) && wants(i.partner_wish) && <span className="pickrow__tag">хотите оба</span>}</span>
                </button>
                <Btn sm kind={mine ? 'soft' : 'primary'} onClick={() => pick(i.id)}>{mine ? 'Мой выбор' : 'Сюда'}</Btn>
              </div>)
          })}
        </div>
      )}
      {r.my_vote && r.my_vote > 0 && !theirs ? <p className="caption muted" style={{ textAlign: 'center', margin: 0 }}>Ждём {ncase(partner, 'acc')}. Выбор можно поменять.</p> : null}
      <Btn kind="ghost" block icon="shuffle" onClick={() => { act('reshuffle').catch(() => {}) }}>Показать другие</Btn>
    </div>
  )
}

// ---------- 3. confirm ----------
function ConfirmStage({ r, act, partner, home, onPlace, onRound }: { r: Round; act: Act; partner: string; home: Home; onPlace: () => void; onRound: (x: R) => void }) {
  return (
    <div className="col">
      <div className="meet rise" style={{ margin: '4px auto 4px' }}><i /><i /></div>
      <h1 className="display rise" style={{ textAlign: 'center' }}>Совпало!</h1>
      <p className="callout muted rise" style={{ textAlign: 'center', margin: 0 }}>Выбор поменялся — встреча будет в плане, когда «да» скажете оба.</p>
      <ProposalsBar r={r} home={home} onRound={onRound} />
      <Ticket r={r} />
      {r.chosen && <Places idea={r.chosen} me={home.me} partner={home.partner} compact onChange={onPlace} />}
      <RouteBlock r={r} home={home} onRound={onRound} />
      <div className="row caption" style={{ justifyContent: 'center', gap: 16 }}>
        <span className="row" style={{ gap: 6 }}><span className="dot" style={{ ['--c' as any]: 'var(--success)', opacity: r.confirmed ? 1 : .3 }} />Ты: {r.confirmed ? 'да' : 'ждём'}</span>
        <span className="row" style={{ gap: 6 }}><span className="dot" style={{ ['--c' as any]: 'var(--success)', opacity: r.partner_confirmed ? 1 : .3 }} />{partner}: {r.partner_confirmed ? 'да' : 'ждём'}</span>
      </div>
      {!r.confirmed ? <Btn kind="accent" block shine icon="check" onClick={() => act('confirm').then(() => {}).catch(() => {})}>Подтверждаю</Btn>
        : <div className="pill pill--ok" style={{ alignSelf: 'center', height: 36, padding: '0 14px' }}><Icon n="check" s={16} w={2.6} />Готово — ждём {ncase(partner, 'acc')}</div>}
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
// the tickets themselves, kept with the meeting: a file or a link — sent to the partner, linked from the calendar
function TicketsFile({ r, home, onRound }: { r: Round; home: Home; onRound: (x: R) => void }) {
  const [busy, setBusy] = useState(false)
  const [link, setLink] = useState('')
  const pick = useRef<HTMLInputElement>(null)
  const f = r.tickets_file
  const send = async (body: FormData | { url: string }) => {
    setBusy(true); haptic.tap()
    try { onRound(body instanceof FormData ? await upload<R>(`/rounds/${r.id}/tickets/file`, body) : await post<R>(`/rounds/${r.id}/tickets/file`, body)); haptic.ok(); toast('Сохранил и отправил ' + ncase(home.partner?.name || 'партнёру', 'dat')) }
    catch (e) { toast(humanError(e)) } finally { setBusy(false) }
  }
  if (f) return <Btn kind="soft" block icon="link" onClick={() => openLink(f.url)}>{f.kind === 'link' ? 'Открыть билеты' : 'Билеты' + (f.name ? ' · ' + f.name : '')}</Btn>
  return <div className="col" style={{ gap: 8 }}>
    <input ref={pick} type="file" accept="image/*,application/pdf,.pkpass" hidden onChange={e => { const x = e.target.files?.[0]; if (!x) return; const d = new FormData(); d.append('file', x, x.name); send(d); e.target.value = '' }} />
    <Btn kind="surface" block icon="plus" onClick={() => pick.current?.click()} disabled={busy}>Прикрепить билеты — файл или скрин</Btn>
    <form className="places__add" onSubmit={e => { e.preventDefault(); if (link.trim()) send({ url: link.trim() }) }}>
      <input className="input" placeholder="или ссылка на билеты" value={link} onChange={e => setLink(e.target.value)} aria-label="Ссылка на билеты" />
      <button className="places__send" type="submit" disabled={!link.trim() || busy} aria-label="Сохранить ссылку"><Icon n="check" s={18} w={2.6} /></button>
    </form>
    <p className="caption muted" style={{ margin: 0 }}>Перешлю {ncase(home.partner?.name || 'партнёру', 'dat')} и добавлю в событие календаря. Можно и в боте — просто пришли туда файл.</p>
  </div>
}

function PlannedStage({ r, act, home, onPlace, onRound }: { r: Round; act: Act; home: Home; onPlace: () => void; onRound: (x: R) => void }) {
  const past = (r.starts_at || 0) * 1000 < Date.now()
  return (
    <div className="col">
      {!past && <>
        <div className="meet rise" style={{ margin: '4px auto 0', transform: 'scale(.7)' }}><i /><i /></div>
        <h1 className="display rise" style={{ textAlign: 'center', margin: 0 }}>Совпало! Идём</h1>
        <p className="callout muted rise" style={{ textAlign: 'center', margin: 0 }}>Встреча в плане у вас обоих. Передумали — «Перенести» или «Отменить» внизу.</p>
      </>}
      {!past && <ProposalsBar r={r} home={home} onRound={onRound} />}
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
          {r.tickets ? <>
            <p className="callout" style={{ margin: 0 }}>✓ {r.tickets === 'me' ? 'Ты ' + agreed(home.me?.name, 'взял') : `${home.partner?.name || 'Партнёр'} ${agreed(home.partner?.name, 'взял')}`} {r.chosen?.category === 'food' ? 'стол' : 'билеты'} — всё готово.</p>
            <TicketsFile r={r} home={home} onRound={onRound} />
          </> : <>
            <p className="callout" style={{ margin: 0 }}><b>{r.book.label}</b> — места разбирают, лучше сразу. Отметь, когда возьмёшь, — чтобы не купить дважды.</p>
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
        <ExactTime r={r} act={act} partner={home.partner?.name} />
        <div className="row"><Btn kind="soft" block icon="cal" onClick={() => act('other-time').then(() => {}).catch(() => {})}>Другой день</Btn>
          <Btn kind="ghost" block onClick={() => act('cancel').then(() => go('', true)).catch(() => {})}>Отменить</Btn></div>
      </>}
    </div>
  )
}

// the real time of an agreed meeting (tickets say 14:00, the slot said «днём»): it moves, nothing is agreed again
function ExactTime({ r, act, partner }: { r: Round; act: Act; partner?: string }) {
  const [open, setOpen] = useState(false)
  const [v, setV] = useState(r.starts_at ? hm(r.starts_at) : '')
  if (!r.starts_at) return null
  const save = () => act('time', { at: v }).then(() => { setOpen(false); haptic.ok(); toast(`Время — ${v}. ${partner || 'Партнёр'} знает, календарь поправлен`) }).catch(() => {})
  return open
    ? <div className="row rise" style={{ gap: 8 }} data-testid="exact-time">
        <input className="input" type="time" step={300} value={v} onChange={e => setV(e.target.value)} aria-label="Точное время" style={{ flex: 1 }} />
        <Btn kind="primary" onClick={save} disabled={!v || v === hm(r.starts_at)}>Сохранить</Btn>
      </div>
    : <Btn kind="soft" block icon="clock" onClick={() => { haptic.tap(); setOpen(true) }}>Точное время · {hm(r.starts_at)}</Btn>
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

// no busy times yet: connect right here, without leaving the time step (the slots refresh by themselves)
function BusyChip() {
  const [open, setOpen] = useState(false)
  return (<>
    <button className="wkchip" style={{ alignSelf: 'flex-start' }} data-testid="busy-chip" onClick={() => setOpen(true)}><Icon n="clock" s={14} />Показать мою занятость из календаря</button>
    <CalSheet open={open} start="busy" onClose={() => setOpen(false)} />
  </>)
}
