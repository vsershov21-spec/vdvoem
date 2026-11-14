import { useState } from 'react'
import { useLoad, useOn, emit } from '../lib/data'
import { post, humanError } from '../lib/api'
import { go, back } from '../lib/router'
import { haptic, openLink } from '../lib/tg'
import { when as whenText, ncase } from '../lib/format'
import type { Idea, Round } from '../lib/types'
import type { HomeV2 } from './types'
import { Sheet, Pic, say } from './ui'

type MP = { id: number; title: string; sub: string; mine: boolean; state: 'wait' | 'no'; start?: number; idea?: number }
type MeetRes = { round: Round; proposals: { time?: MP; stop?: MP }; buying?: '' | 'me' | 'partner' }
export type FreeRes = { days: { day: string; label: string }[]; mine: string[]; partner: string[]; common: string[]; busy: string[]; partner_busy: string[]; today: string }
const PARTS = ['Утро', 'День', 'Вечер'], HOURS = ['11:00', '14:00', '19:00']
const fem = (n: string) => /[ая]$/i.test(n) ? 'а' : ''

// The meeting card: status, when, where, «а потом», tickets. A time and a place are offered and answered;
// nothing gets into the plans on one person's word.
export function Meet({ id, home, from }: { id: number; home: HomeV2 | null; from: string }) {
  const { data, refresh, set } = useLoad<MeetRes>('/meet/' + id, 20000)
  useOn('home', refresh)
  const [sheet, setSheet] = useState<'' | 'time' | 'stop' | 'cancel'>('')
  const [busy, setBusy] = useState(false)
  const pn = home?.partner?.name || 'партнёр'
  const head = <div className="subh"><button className="back" aria-label="Назад" onClick={() => back('')}>‹ {from}</button><b>Встреча</b><span /></div>
  if (!data) return <div className="scr mcard">{head}</div>
  const r = data.round, i = r.chosen, tp = data.proposals.time, sp = data.proposals.stop
  const planned = r.state === 'planned', past = planned && !!r.starts_at && r.starts_at * 1000 < Date.now()
  const closed = r.state === 'cancelled' || r.state === 'expired' || r.state === 'done'
  const status = r.state === 'cancelled' ? 'отменена' : r.state === 'done' ? 'прошла' : past ? 'прошла' : tp?.state === 'wait' ? (tp.mine ? 'ждём ' + ncase(pn, 'acc') : 'ждёт твоего ответа') : planned ? 'в планах' : tp?.state === 'no' ? 'время не подошло' : 'без времени'
  const run = async (f: () => Promise<unknown>, ok?: string) => {
    setBusy(true)
    try { const res = await f(); if (res && (res as MeetRes).round) set(res as MeetRes); else refresh(); haptic.ok(); if (ok) say(ok); emit('home') }
    catch (e) { say(humanError(e)) } finally { setBusy(false) }
  }
  const answer = (p: MP, yes: boolean, ok: string) => run(() => post(`/proposals/${p.id}/answer`, { yes }), ok)
  const stops = (r.route || []).filter(s => s.state === 'accepted')
  return (
    <div className="scr mcard" data-testid="v2-meet">
      {head}
      {i ? <Pic idea={i} style={{ minHeight: 150 }}>{i.title}<small data-testid="meet-status">{status}</small></Pic> : <div className="pic t1" style={{ minHeight: 120 }}>Встреча<small>{status}</small></div>}
      {past && !closed && <After round={r} onDone={refresh} />}
      {tp?.state === 'wait' && !tp.mine && <div className="v-card prop" data-testid="time-offer"><b style={{ color: 'var(--accent)', fontSize: 12.5 }}>{pn} зовёт</b><b>{tp.sub}</b>
        <div className="v-row"><button className="b a s" disabled={busy} onClick={() => answer(tp, true, 'Встреча в планах у обоих')}>Да</button><button className="b s" onClick={() => setSheet('time')}>Другое время</button><button className="b s ghost" disabled={busy} onClick={() => answer(tp, false, pn + ' увидит отказ')}>Не могу</button></div></div>}
      {sp?.state === 'wait' && !sp.mine && <div className="v-card prop" data-testid="stop-offer"><b style={{ color: 'var(--accent)', fontSize: 12.5 }}>{pn} предлагает после</b>
        <div className="v-row"><span style={{ flex: 1, minWidth: 0 }}><b>{sp.title}</b></span><button className="b a s" disabled={busy} onClick={() => answer(sp, true, 'Место в маршруте')}>Да</button><button className="b s" disabled={busy} onClick={() => answer(sp, false, pn + ' увидит «нет»')}>Нет</button></div></div>}
      {!closed && <>
        <div className="blk"><label>Когда</label><div className="v-card" data-testid="meet-when">
          {tp?.state === 'wait' ? <><div className="v-row"><b>{tp.sub}</b><span className="sp" />{tp.mine && <button className="back" onClick={() => setSheet('time')}>Изменить</button>}</div>
            <p className="m">{tp.mine ? `Ждём ответа ${ncase(pn, 'gen')}. Строка на главной и в боте.` : 'Ответь выше.'}{planned && r.starts_at ? ' Пока действует ' + whenText(r.starts_at) + '.' : ''}</p></>
            : planned && r.starts_at ? <><div className="v-row"><b>{whenText(r.starts_at)}</b><span className="sp" />{!past && <button className="back" onClick={() => setSheet('time')}>Перенести</button>}</div><p className="m">Оба согласны. В календаре телефона у обоих.</p></>
              : tp?.state === 'no' ? <><p>{pn} не может {tp.sub}</p><button className="b p w" onClick={() => { post(`/proposals/${tp.id}/seen`).catch(() => {}); setSheet('time') }}>Другое время</button></>
                : <><p className="m">Время не выбрано. Предложи, {pn} ответит.</p><button className="b p w" data-testid="offer-time" onClick={() => setSheet('time')}>Предложить время</button></>}
        </div></div>
        <div className="blk"><label>Где</label><div className="v-card"><b>{i?.address || i?.place?.title || 'адрес не указан'}</b>{i?.maps && <button className="back" style={{ alignSelf: 'flex-start' }} onClick={() => openLink(i.maps)}>открыть на карте ↗</button>}</div></div>
        <div className="blk"><label>А потом</label><div className="v-card" data-testid="meet-after">
          {stops.map(s => <div className="v-row" key={s.id}><Pic idea={s.idea} className="th" /><span style={{ flex: 1, minWidth: 0 }}><b>{s.idea.title}</b><p className="m">договорились</p></span></div>)}
          {sp?.mine && <div className="v-row"><span style={{ flex: 1, minWidth: 0 }}><b>{sp.title}</b><p className="m">{sp.state === 'wait' ? 'ждём ' + ncase(pn, 'acc') : pn + ': нет'}</p></span></div>}
          {!stops.length && !sp?.mine && <p className="m">Место выбираете вы, второй подтверждает.</p>}
          <button className="b s" style={{ alignSelf: 'flex-start' }} data-testid="add-stop" onClick={() => { if (sp?.mine && sp.state === 'no') post(`/proposals/${sp.id}/seen`).catch(() => {}); setSheet('stop') }}>{stops.length || sp?.mine ? 'Другое место' : 'Выбрать место'}</button>
        </div></div>
        {planned && !past && <div className="blk"><label>Билеты</label><div className="v-card" data-testid="meet-tickets">
          {r.tickets ? <b>Билеты у {r.tickets === 'me' ? 'тебя' : ncase(pn, 'gen')}</b>
            : data.buying ? <div className="v-row"><b>{data.buying === 'me' ? 'Покупаешь ты' : 'Покупает ' + pn}</b><span className="sp" />{data.buying === 'me' && <button className="b a s" disabled={busy} onClick={() => run(() => post(`/rounds/${r.id}/tickets`), 'Билеты отмечены')}>Куплены</button>}</div>
              : <><p className="m">Кто покупает?</p><div className="v-row"><button className="b a s" disabled={busy} data-testid="buy-me" onClick={() => run(() => post(`/meet/${r.id}/buy`), pn + ' видит: покупаешь ты')}>Куплю я</button><button className="b s" disabled={busy} onClick={() => run(() => post(`/rounds/${r.id}/tickets`), 'Билеты отмечены')}>Уже есть</button>
                {r.book && <button className="back" onClick={() => openLink(r.book!.url)}>{r.book.label} ↗</button>}</div></>}
        </div></div>}
        {!past && <button className="b ghost w" data-testid="cancel-meet" onClick={() => setSheet('cancel')}>Отменить встречу</button>}
      </>}
      {closed && <div className="v-card"><p>{r.state === 'cancelled' ? 'Встреча отменена.' : 'Эта встреча уже в истории.'}</p><button className="b w" onClick={() => go('plans')}>К планам</button></div>}
      <Sheet open={sheet === 'time'} onClose={() => setSheet('')} testid="time-sheet">
        <TimeSheet title={(planned ? 'Перенести: ' : 'Когда: ') + (i?.title || '')} partner={pn} note={planned ? `Старое время действует, пока ${pn} не ответит «Да».` : 'Встреча попадёт в планы после ответа «Да».'}
          onPick={(day, part, at) => { setSheet(''); run(() => post(`/meet/${r.id}/time`, { day, part, at }), `Отправлено ${ncase(pn, 'dat')}: на главную и в бот`) }} />
      </Sheet>
      <Sheet open={sheet === 'stop'} onClose={() => setSheet('')} testid="stop-sheet">
        <StopSheet main={i} partner={pn} onPick={b => { setSheet(''); run(() => post(`/meet/${r.id}/stop`, b), `Отправлено ${ncase(pn, 'dat')}`) }} />
      </Sheet>
      <Sheet open={sheet === 'cancel'} onClose={() => setSheet('')} testid="cancel-sheet">
        <h3>Отменить встречу?</h3><p>{i?.title}{planned && r.starts_at ? ' · ' + whenText(r.starts_at) : ''}</p>
        <ul className="ul m"><li>{pn} получит строку на главной и сообщение в боте.</li><li>Встреча исчезнет из календаря телефона у обоих.</li>{stops.length > 0 && <li>Место «а потом» отменится вместе с ней.</li>}{r.tickets && <li>Билеты останутся у того, кто их купил: их нужно вернуть самим.</li>}</ul>
        <button className="b w red" data-testid="cancel-yes" onClick={async () => { try { await post(`/meet/${r.id}/cancel`); say(`Встреча отменена. ${pn} узнает`); emit('home'); go('plans') } catch (e) { say(humanError(e)) } }}>Отменить встречу</button>
        <button className="b w" onClick={() => setSheet('')}>Оставить</button>
      </Sheet>
    </div>)
}

// the grid of days and parts of the day: where the partner is free, where I am busy; a slot and a time
export function TimeSheet({ title, partner, note, onPick }: { title: string; partner: string; note: string; onPick: (day: string, part: number, at: string) => void }) {
  const { data } = useLoad<FreeRes>('/free')
  const [slot, setSlot] = useState<{ day: string; part: number; label: string } | null>(null)
  const [at, setAt] = useState('')
  if (!data) return <h3>{title}</h3>
  const time = at || (slot ? HOURS[slot.part] : '')
  return (<>
    <h3>{title}</h3>
    <p className="m">Синяя рамка: {partner} свободн{fem(partner) || 'ен'}. Штриховка: ты занят.</p>
    <div className="grid4"><span />{PARTS.map(p => <b key={p}>{p}</b>)}
      {data.days.map(d => [<span key={d.day}>{d.label}</span>, ...[0, 1, 2].map(p => {
        const k = d.day + ':' + p, bz = data.busy.includes(d.day), on = slot?.day === d.day && slot.part === p
        return <button key={k} className={'slot' + (on ? ' on' : bz ? ' busy' : '') + (data.partner.includes(k) ? ' their' : '')} aria-label={d.label + ' ' + PARTS[p]} aria-pressed={on}
          onClick={() => { setSlot({ day: d.day, part: p, label: d.label }); setAt('') }}>{on ? time : ''}</button>
      })])}
    </div>
    {slot && <label className="fld"><small>Точное время</small><input type="time" value={time} onChange={e => setAt(e.target.value)} /></label>}
    <button className="b a w" disabled={!slot} style={slot ? undefined : { opacity: .45 }} data-testid="offer-send" onClick={() => slot && onPick(slot.day, slot.part, time)}>{slot ? `Предложить ${ncase(partner, 'dat')}: ${slot.label}, ${time}` : 'Выбери время'}</button>
    <p className="m">{note}</p>
  </>)
}

function StopSheet({ main, partner, onPick }: { main?: Idea; partner: string; onPick: (b: { idea?: number; text?: string }) => void }) {
  const { data } = useLoad<{ ideas: Idea[] }>('/ideas')
  const [text, setText] = useState('')
  const w = (x?: string) => x === 'want' || x === 'very'
  const all = (data?.ideas || []).filter(i => i.kind !== 'chore' && i.id !== main?.id)
  const both = all.filter(i => w(i.my_wish) && w(i.partner_wish)).slice(0, 4), mine = all.filter(i => w(i.my_wish) && !w(i.partner_wish) && i.ours).slice(0, 3)
  const scout = all.filter(i => !i.ours && !both.includes(i)).slice(0, 3)
  const group = (l: string, xs: Idea[]) => xs.length > 0 && <div className="blk"><label>{l}</label>{xs.map(i =>
    <button className="in" key={i.id} data-testid="stop-option" onClick={() => onPick({ idea: i.id })}><Pic idea={i} className="th" /><span><b>{i.title}</b><small>{i.address || ''}</small></span>＋</button>)}</div>
  return (<>
    <h3>Куда после{main ? ` «${main.title}»` : ''}?</h3><p className="m">Выбери место. {partner} подтвердит.</p>
    {group('Вы оба хотите', both)}{group('Твои идеи', mine)}{group('Нашёл скаут', scout)}
    <label className="fld"><small>Своё: ссылка или название</small><input value={text} onChange={e => setText(e.target.value)} placeholder="Кафе у Патриарших" /></label>
    <button className="b p w" disabled={!text.trim()} data-testid="stop-own" onClick={() => onPick({ text })}>Предложить это место</button>
    <button className="b w" onClick={() => go('assistant')}>✦ Спросить помощника: что рядом</button>
  </>)
}

// «Как прошло»: each one answers for themselves
function After({ round, onDone }: { round: Round; onDone: () => void }) {
  const [busy, setBusy] = useState(false)
  const rate = async (k: 'like' | 'no' | 'skip') => {
    setBusy(true)
    try {
      await post(`/rounds/${round.id}/done`, { answer: k === 'skip' ? 'no' : 'yes' })
      if (k !== 'skip') await post(`/rounds/${round.id}/again`, { answer: k === 'like' ? 'yes' : 'no' })
      say('Записал в историю'); emit('home'); onDone()
    } catch (e) { say(humanError(e)) } finally { setBusy(false) }
  }
  return (
    <div className="v-card" data-testid="after"><h3>Как тебе?</h3><p className="m">Каждый отвечает за себя. Ответ уточняет твой вкус и попадает в историю.</p>
      <div className="v-row" style={{ flexWrap: 'wrap' }}><button className="b" disabled={busy} onClick={() => rate('like')}>Понравилось</button><button className="b" disabled={busy} onClick={() => rate('no')}>Не очень</button><button className="b" disabled={busy} onClick={() => rate('skip')}>Не ходили</button></div></div>)
}
