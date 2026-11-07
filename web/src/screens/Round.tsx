import { useEffect, useRef, useState } from 'react'
import type { Home, Idea, Round } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, humanError, ApiError } from '../lib/api'
import { go } from '../lib/router'
import { dayLabel, hm, money, dur, relLeft, when } from '../lib/format'
import { haptic, download } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Btn, Cover, Empty, Marks, Skel, SubHeader, burst, toast } from '../ui/kit'
import { Places } from '../ui/Places'
import { openLink } from '../lib/tg'

type R = { round: Round }

export function RoundScreen({ id, home }: { id: number; home: Home | null }) {
  const { data, set, refresh } = useLoad<R>('/rounds/' + id, 5000)
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
    case 'time': body = <TimeStage r={r} act={act} partner={partner} />; break
    case 'vote': body = <VoteStage r={r} act={act} partner={partner} home={home} />; break
    case 'confirm': body = <ConfirmStage r={r} act={act} partner={partner} home={home} onPlace={refresh} />; break
    case 'planned': body = <PlannedStage r={r} act={act} home={home} onPlace={refresh} />; break
    case 'done': body = <DoneStage r={r} act={act} />; break
    default: body = <Closed r={r} />
  }
  return <><SubHeader title={titles[r.state]} right={['time', 'vote', 'confirm'].includes(r.state) ? <span className="caption muted">{relLeft(r.expires_at)}</span> : undefined} />{body}</>
}

const titles: Record<string, string> = { time: 'Когда удобно', vote: 'Выбираем', confirm: 'Совпало', planned: 'Встреча', done: 'Встреча', expired: 'Выбор закрыт', cancelled: 'Отменено' }
type Act = (a: string, b?: object) => Promise<R>

// ---------- 1. time ----------
const PARTS: [number, string, string][] = [[0, 'Утро', 'sunrise'], [1, 'День', 'sun'], [2, 'Вечер', 'moon']]

function TimeStage({ r, act, partner }: { r: Round; act: Act; partner: string }) {
  const [sel, setSel] = useState<Set<string>>(new Set(r.my_slots))
  const [editing, setEditing] = useState(!r.sent)
  const paint = useRef<null | boolean>(null)
  const toggle = (k: string, on?: boolean) => setSel(s => { const n = new Set(s); const v = on ?? !n.has(k); if (v === n.has(k)) return s; v ? n.add(k) : n.delete(k); haptic.select(); return n })
  const send = async () => { await act('slots', { slots: [...sel] }); setEditing(false) }
  const noOverlap = r.sent && r.partner_sent
  if (!editing) {
    return (
      <div className="col">
        {r.chosen && <ChosenStrip i={r.chosen} />}
        <div className="frame rise"><div className="frame__in" style={{ textAlign: 'center' }}>
          <div className="meet" style={{ margin: '8px auto 16px' }}><i /><i /></div>
          {noOverlap ? <>
            <h2 className="title">Время не совпало</h2>
            <p className="callout muted">Ни одного общего слота. Добавьте ещё варианты — хотя бы один общий.</p>
          </> : <>
            <h2 className="title">Ждём {partner}</h2>
            <p className="callout muted">Как только {partner} отметит время, я найду пересечение и пришлю варианты вам обоим.</p>
          </>}
          <Btn kind="soft" sm icon="edit" onClick={() => setEditing(true)}>Изменить моё время</Btn>
        </div></div>
      </div>
    )
  }
  return (
    <div className="col">
      {r.chosen && <ChosenStrip i={r.chosen} />}
      <p className="callout muted rise" style={{ margin: 0 }}>Отметь всё, когда можешь. {partner} не увидит твои отметки — только общее время.</p>
      <div className="slots rise" style={{ ['--d' as any]: 1 }} onPointerUp={() => { paint.current = null }} onPointerLeave={() => { paint.current = null }}>
        <span />{PARTS.map(([, l, ic]) => <span key={l} className="slots__h row" style={{ justifyContent: 'center', gap: 4 }}><Icon n={ic} s={14} w={2} />{l}</span>)}
        {r.days.map(d => { const L = dayLabel(d); return [
          <div key={d} className="slots__day"><b>{L.wd}</b><span>{L.num} {L.mon}</span></div>,
          ...PARTS.map(([p]) => { const k = `${d}:${p}`; return (
            <button key={k} className="cell" aria-pressed={sel.has(k)} aria-label={`${L.long} ${PARTS[p][1]}`} style={{ ['--c' as any]: 'var(--person-a)' }}
              onPointerDown={e => { (e.target as HTMLElement).releasePointerCapture?.(e.pointerId); paint.current = !sel.has(k); toggle(k, paint.current) }}
              onPointerEnter={() => { if (paint.current !== null) toggle(k, paint.current) }}>
              <Icon n="check" s={22} w={2.6} />
            </button>) }),
        ] })}
      </div>
      <p className="caption muted" style={{ margin: 0 }}>Утро 9–13 · день 13–18 · вечер 18–23. Можно вести пальцем по клеткам.</p>
      <Btn kind="primary" block onClick={send} disabled={sel.size === 0}>{sel.size === 0 ? 'Отметь хотя бы одно' : `Готово · ${sel.size} ${sel.size === 1 ? 'слот' : sel.size < 5 ? 'слота' : 'слотов'}`}</Btn>
    </div>
  )
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
          <p className="callout muted">Ты выбрал(а) «{mine?.title || 'ни один'}». Голос {partner} откроется, когда он(а) проголосует — так честнее.</p>
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
function ConfirmStage({ r, act, partner, home, onPlace }: { r: Round; act: Act; partner: string; home: Home; onPlace: () => void }) {
  return (
    <div className="col">
      <div className="meet rise" style={{ margin: '4px auto 4px' }}><i /><i /></div>
      <h1 className="display rise" style={{ textAlign: 'center' }}>Совпало!</h1>
      <p className="callout muted rise" style={{ textAlign: 'center', margin: 0 }}>Встреча запланируется, когда подтвердите оба. Молчание не считается согласием.</p>
      <Ticket r={r} />
      {r.chosen && <Places idea={r.chosen} me={home.me} partner={home.partner} compact onChange={onPlace} />}
      <div className="row caption" style={{ justifyContent: 'center', gap: 16 }}>
        <span className="row" style={{ gap: 6 }}><span className="dot" style={{ ['--c' as any]: 'var(--success)', opacity: r.confirmed ? 1 : .3 }} />Ты: {r.confirmed ? 'да' : 'ждём'}</span>
        <span className="row" style={{ gap: 6 }}><span className="dot" style={{ ['--c' as any]: 'var(--success)', opacity: r.partner_confirmed ? 1 : .3 }} />{partner}: {r.partner_confirmed ? 'да' : 'ждём'}</span>
      </div>
      {!r.confirmed ? <Btn kind="accent" block shine icon="check" onClick={() => act('confirm').then(() => {}).catch(() => {})}>Подтверждаю</Btn>
        : <div className="pill pill--ok" style={{ alignSelf: 'center', height: 36, padding: '0 14px' }}><Icon n="check" s={16} w={2.6} />Ты подтвердил(а) — ждём {partner}</div>}
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
        {!!i.price_min && <div className="trow"><Icon n="wallet" s={20} /><span className="callout">≈ {money(i.price_min)} на двоих</span></div>}
        {(i.category === 'food' || i.category === 'culture') && <div className="trow"><Icon n="bell" s={20} /><span className="callout">Заранее: забронировать или купить билеты</span></div>}
      </div>
    </div>
  )
}

// ---------- 4. planned ----------
function PlannedStage({ r, act, home, onPlace }: { r: Round; act: Act; home: Home; onPlace: () => void }) {
  const past = (r.starts_at || 0) * 1000 < Date.now()
  return (
    <div className="col">
      <Ticket r={r} />
      {!past && r.chosen && <Places idea={r.chosen} me={home.me} partner={home.partner} compact onChange={onPlace} />}
      {past ? <>
        <h2 className="title" style={{ textAlign: 'center', marginTop: 8 }}>Сходили?</h2>
        <div className="row"><Btn kind="accent" block onClick={() => act('done', { answer: 'yes' }).then(burst).catch(() => {})}>Да!</Btn>
          <Btn kind="soft" block onClick={() => act('done', { answer: 'moved' }).then(() => {}).catch(() => {})}>Перенесли</Btn></div>
        <Btn kind="ghost" block onClick={() => act('done', { answer: 'no' }).then(() => {}).catch(() => {})}>Не получилось</Btn>
      </> : <>
        <Btn kind="primary" block icon="cal" onClick={() => download(`${location.origin}/api/rounds/${r.id}/ics`, 'vdvoem.ics')}>В календарь</Btn>
        <p className="caption muted" style={{ textAlign: 'center', margin: 0 }}>Напомню за 2 часа. После встречи спрошу, как прошло.</p>
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
