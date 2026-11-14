import { useState } from 'react'
import { useLoad, useOn, emit } from '../lib/data'
import { post, patch, humanError } from '../lib/api'
import { go, back } from '../lib/router'
import { haptic } from '../lib/tg'
import { ncase } from '../lib/format'
import type { HomeV2, ChoreSummary } from './types'
import { Sheet, Empty, say } from './ui'

export type Chore = { id: number; title: string; note: string; who: 'me' | 'partner' | 'any'; who_name: string; due?: number; due_time: boolean; repeat: '' | 'week' | 'month'
  state: 'open' | 'ask' | 'declined' | 'done'; section: 'ask' | 'overdue' | 'today' | 'later' | 'done'; actions: string[]; asked_by?: 'me' | 'partner'; done_by?: string; done_at?: number }
type Res = { chores: Chore[]; summary: ChoreSummary }
const REPEAT: Record<string, string> = { '': 'нет', week: 'каждую неделю', month: 'каждый месяц' }
const fem = (n: string) => /[ая]$/i.test(n) ? 'а' : ''
export const dueText = (c: { due?: number; due_time: boolean }) => {
  if (!c.due) return 'без срока'
  const d = new Date(c.due * 1000), t = new Date(), day = (x: Date) => Math.floor(new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime() / 864e5)
  const k = day(d) - day(t), base = k === 0 ? 'сегодня' : k === 1 ? 'завтра' : d.toLocaleDateString('ru', { weekday: 'short', day: 'numeric', month: 'short' })
  return base + (c.due_time ? ', ' + d.toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' }) : '')
}
const doneText = (c: Chore) => `${c.done_by} · ${c.done_at ? new Date(c.done_at * 1000).toLocaleDateString('ru', { weekday: 'short', day: 'numeric', month: 'short' }) : ''}`
const LABEL = (pn: string): Record<string, string> => ({ done: 'Сделано', pass: 'Передать ' + ncase(pn, 'dat'), nudge: 'Напомнить ' + ncase(pn, 'dat'), take: 'Беру', move: 'Перенести на завтра', reopen: 'Вернуть в работу', withdraw: 'Отозвать просьбу', self: 'Беру сам', any: 'Кто угодно', decline: 'Не могу' })
const OK: Record<string, string> = { pass: 'Просьба отправлена', nudge: 'Напоминание ушло в бот', take: 'Дело твоё', move: 'Срок: завтра', reopen: 'Вернул в работу', withdraw: 'Просьба отозвана', self: 'Дело твоё', any: 'Дело общее', decline: 'Дело вернулось автору' }

export async function choreAct(id: number, act: string, after: (r: Res) => void) {
  try {
    const r = await post<Res>(`/chores/${id}/${act}`)
    haptic.ok(); after(r); emit('home')
    if (act === 'done') say('Сделано', () => choreAct(id, 'reopen', after))
    else if (act === 'delete') say('Дело удалено', () => choreAct(id, 'restore', after))
    else if (OK[act]) say(OK[act])
  } catch (e) { say(humanError(e)) }
}

// ---------- the tab: every state of a chore has its place ----------
type Filter = 'all' | 'mine' | 'their' | 'any'
export function Chores({ home }: { home: HomeV2 | null }) {
  const { data, refresh, set } = useLoad<Res>('/chores', 25000)
  useOn('home', refresh)
  const [f, setF] = useState<Filter>('all')
  const [showDone, setShowDone] = useState(false)
  const [add, setAdd] = useState(false)
  const pn = home?.partner?.name || 'партнёр', paired = !!home?.paired
  const pass = (c: Chore) => f === 'all' || (f === 'mine' ? c.who === 'me' : f === 'their' ? c.who === 'partner' : c.who === 'any')
  const all = data?.chores || []
  const sec = (s: string) => all.filter(c => c.section === s && (s === 'ask' || pass(c)))
  const row = (c: Chore) => (
    <div className={'in' + (c.state === 'done' ? ' done' : '')} key={c.id} data-testid="chore-row">
      <button className={'ck' + (c.state === 'done' ? ' on' : '')} aria-label={c.state === 'done' ? 'Вернуть в работу' : 'Сделано'} disabled={c.state !== 'done' && !c.actions.includes('done')}
        onClick={() => choreAct(c.id, c.state === 'done' ? 'reopen' : 'done', set)}>{c.state === 'done' ? '✓' : ''}</button>
      <span style={{ cursor: 'pointer' }} onClick={() => go('chore/' + c.id)}><b>{c.title}</b>
        <small className={c.section === 'overdue' ? 'red' : ''}>{c.state === 'done' ? doneText(c) : `${c.who === 'me' ? 'я' : c.who_name} · ${c.section === 'overdue' ? 'был срок ' : ''}${dueText(c)}${c.repeat ? ' · ' + REPEAT[c.repeat] : ''}`}</small></span>›
    </div>)
  const block = (label: string, s: string, red = false) => { const xs = sec(s); return xs.length > 0 && <div className="blk"><label className={red ? 'red' : ''}>{label}</label>{xs.map(row)}</div> }
  const ask = sec('ask'), done = sec('done')
  const open = sec('overdue').length + sec('today').length + sec('later').length
  return (
    <div className="scr" data-testid="v2-chores">
      <div className="v-row"><h2>Дела</h2><span className="sp" /><button className="b p s" data-testid="add-chore" onClick={() => setAdd(true)}>＋ Дело</button></div>
      <div className="v-seg" role="tablist">{([['all', 'Все'], ['mine', 'Мои'], ...(paired ? [['their', ncase(pn, 'gen')]] : []), ['any', 'Общие']] as [Filter, string][]).map(([k, l]) =>
        <button key={k} role="tab" aria-selected={f === k} className={f === k ? 'on' : ''} onClick={() => setF(k)}>{l}</button>)}</div>
      {ask.length > 0 && <div className="blk"><label>Ждёт ответа</label>{ask.map(c => {
        const mine = c.asked_by === 'me', hot = c.state === 'ask' ? !mine : mine
        return <div className={'q' + (hot ? ' hot' : '')} key={c.id} data-testid="chore-ask">
          <button className="qt" onClick={() => go('chore/' + c.id)}><i>☑</i><span><b>{c.title}</b>
            <small>{c.state === 'ask' ? (mine ? `ждём ответа ${ncase(pn, 'gen')}` : `${pn} просит`) : (mine ? `${pn} не может` : `ты ответил${fem(home?.me.name || '')} «не могу»`)} · {dueText(c)}</small></span></button>
          {c.actions.length > 0 && <div className="qb">{c.actions.map((a, k) => <button key={a} className={'b s' + (k === 0 ? ' a' : '')} onClick={() => choreAct(c.id, a, set)}>{LABEL(pn)[a]}</button>)}</div>}
        </div> })}</div>}
      {block('Просрочено', 'overdue', true)}{block('Сегодня', 'today')}{block('Позже', 'later')}
      {data && !open && !ask.length && <Empty title="Здесь дел нет"><button className="b p w" onClick={() => setAdd(true)}>Добавить дело</button></Empty>}
      {data && <button className="b w" data-testid="done-toggle" onClick={() => setShowDone(!showDone)}>Сделано · {done.length} {showDone ? '▴' : '▾'}</button>}
      {showDone && <>{done.map(row)}<button className="back" style={{ alignSelf: 'flex-start' }} onClick={() => go('history/chores')}>Вся история дел</button></>}
      <Sheet open={add} onClose={() => setAdd(false)} testid="chore-sheet"><ChoreForm partner={paired ? pn : ''} onDone={r => { setAdd(false); if (r) set(r) }} /></Sheet>
    </div>)
}

// ---------- the card: who, deadline, repeat, note; the buttons depend on whose chore it is ----------
export function ChoreCard({ id, home, from }: { id: number; home: HomeV2 | null; from: string }) {
  const { data, set } = useLoad<Res>('/chores', 25000)
  const [edit, setEdit] = useState(false)
  const pn = home?.partner?.name || 'партнёр'
  const c = data?.chores.find(x => x.id === id)
  const head = <div className="subh"><button className="back" aria-label="Назад" onClick={() => back('chores')}>‹ {from}</button><b>Дело</b><span style={{ justifySelf: 'end' }}>{c && c.state !== 'done' && <button className="back" data-testid="edit-chore" onClick={() => setEdit(true)}>Изменить</button>}</span></div>
  if (!c) return <div className="scr">{head}{data && <Empty title="Дела нет"><p className="m">Оно удалено или ушло в историю.</p><button className="b p w" onClick={() => go('chores')}>К делам</button></Empty>}</div>
  const after = (r: Res) => { set(r); if (!r.chores.some(x => x.id === id)) go('chores') }
  return (
    <div className="scr" data-testid="v2-chore">
      {head}<h2>{c.title}</h2>
      {c.state === 'done' ? <div className="v-card"><b>Сделано</b><p className="m">{doneText(c)}. Лежит во вкладке, потом уходит в историю профиля.</p></div>
        : <div className="v-card v-list flds">
          <div className="fld"><small>Делает</small><span>{c.who === 'me' ? 'я' : c.who_name}{c.state === 'ask' ? ' · ждём ответа' : c.state === 'declined' ? ' · не может' : ''}</span></div>
          <div className="fld"><small>Срок</small><span className={c.section === 'overdue' ? 'red' : ''}>{c.section === 'overdue' ? 'был ' : ''}{dueText(c)}</span></div>
          <div className="fld"><small>Повтор</small><span>{REPEAT[c.repeat]}</span></div>
          {c.note && <div className="fld"><small>Заметка</small><span>{c.note}</span></div>}
        </div>}
      {c.actions.map((a, k) => <button key={a} className={'b w' + (k === 0 ? ' a' : '')} data-testid={'act-' + a} onClick={() => choreAct(c.id, a, after)}>{LABEL(pn)[a]}</button>)}
      <Sheet open={edit} onClose={() => setEdit(false)} testid="chore-sheet"><ChoreForm partner="" chore={c} onDone={r => { setEdit(false); if (r) after(r) }} /></Sheet>
    </div>)
}

const dayStart = (k: number) => { const d = new Date(); return Math.floor(new Date(d.getFullYear(), d.getMonth(), d.getDate() + k).getTime() / 1000) }
const localDT = (t?: number) => { if (!t) return ''; const d = new Date(t * 1000 - new Date().getTimezoneOffset() * 60000); return d.toISOString().slice(0, 16) }

function ChoreForm({ partner, chore, onDone }: { partner: string; chore?: Chore; onDone: (r?: Res) => void }) {
  const [title, setTitle] = useState(chore?.title || '')
  const [note, setNote] = useState(chore?.note || '')
  const [who, setWho] = useState<'me' | 'partner' | 'any'>(chore ? (chore.who === 'partner' ? 'me' : chore.who) : 'me')
  const [due, setDue] = useState<number | 0>(chore?.due || 0)
  const [time, setTime] = useState(!!chore?.due_time)
  const [exact, setExact] = useState(false)
  const [repeat, setRepeat] = useState(chore?.repeat || '')
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    const b = { title, note, who, due: due || null, has_due: true, due_time: time && !!due, repeat }
    try {
      const r = chore ? await patch<Res>('/chores/' + chore.id, b) : await post<Res>('/chores', b)
      emit('home'); say(chore ? 'Сохранено' : who === 'partner' ? `Просьба ушла ${ncase(partner, 'dat')}` : 'Дело добавлено'); onDone(r)
    } catch (e) { say(humanError(e)) } finally { setBusy(false) }
  }
  const chip = (on: boolean, l: string, f: () => void) => <button key={l} className={'v-chip' + (on ? ' on' : '')} aria-pressed={on} onClick={f}>{l}</button>
  const isDay = (k: number) => !time && due === dayStart(k)
  return (<>
    <h3>{chore ? 'Изменить дело' : 'Новое дело'}</h3>
    <label className="fld"><small>Что сделать</small><input autoFocus={!chore} value={title} onChange={e => setTitle(e.target.value)} maxLength={120} placeholder="Забрать торт" /></label>
    {(!chore || chore.state === 'open') && <div className="blk"><label>Кто</label><div className="v-row">
      {chip(who === 'me', 'Я', () => setWho('me'))}{partner && chip(who === 'partner', partner, () => setWho('partner'))}{chip(who === 'any', 'Кто угодно', () => setWho('any'))}</div></div>}
    <div className="blk"><label>Срок</label><div className="v-row" style={{ flexWrap: 'wrap' }}>
      {chip(isDay(0), 'Сегодня', () => { setDue(dayStart(0)); setTime(false); setExact(false) })}{chip(isDay(1), 'Завтра', () => { setDue(dayStart(1)); setTime(false); setExact(false) })}
      {chip(exact || (!!due && !isDay(0) && !isDay(1)), 'Дата и время', () => setExact(true))}{chip(!due && !exact, 'Без срока', () => { setDue(0); setTime(false); setExact(false) })}</div>
      {(exact || (!!due && !isDay(0) && !isDay(1))) && <label className="fld"><small>Когда</small><input type="datetime-local" value={localDT(due || dayStart(1) + 12 * 3600)} onChange={e => { const t = Math.floor(new Date(e.target.value).getTime() / 1000); if (t) { setDue(t); setTime(true) } }} /></label>}</div>
    <div className="blk"><label>Повтор</label><div className="v-row" style={{ flexWrap: 'wrap' }}>{(['', 'week', 'month'] as const).map(r => chip(repeat === r, r ? REPEAT[r] : 'Нет', () => setRepeat(r)))}</div></div>
    <label className="fld"><small>Заметка</small><textarea rows={2} value={note} onChange={e => setNote(e.target.value)} /></label>
    <button className="b p w" disabled={busy || !title.trim()} data-testid="chore-save" onClick={save}>{chore ? 'Сохранить' : who === 'partner' ? 'Попросить ' + ncase(partner, 'acc') : 'Добавить'}</button>
    {!chore && who === 'partner' && <p className="m">{partner} ответит «Беру» или «Не могу». Откажется: дело вернётся тебе.</p>}
    {chore && <button className="b ghost w red" data-testid="chore-delete" onClick={async () => { try { const r = await post<Res>(`/chores/${chore.id}/delete`); emit('home'); say('Дело удалено', () => { post<Res>(`/chores/${chore.id}/restore`).then(() => emit('home')).catch(() => {}) }); onDone(r) } catch (e) { say(humanError(e)) } }}>Удалить дело</button>}
  </>)
}
