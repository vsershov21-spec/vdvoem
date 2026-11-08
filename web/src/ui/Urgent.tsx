import { useState } from 'react'
import { post, del, humanError } from '../lib/api'
import { emit } from '../lib/data'
import { go } from '../lib/router'
import { haptic } from '../lib/tg'
import { ncase } from '../lib/format'
import { Icon } from './icons'
import { Btn, Sheet, toast } from './kit'

export type UrgentInfo = { by_me: boolean; note: string; nudges: number; since: number } | null
export type UrgentAsk = { idea_id: number; title: string; note?: string; when?: string; fixed: boolean }

// «Важно — пусть ответит»: the partner gets an alert and reminders until they answer
export function AskUrgent({ ideaId, partner, onDone }: { ideaId: number; partner: string; onDone: () => void }) {
  const [open, setOpen] = useState(false)
  const [note, setNote] = useState('')
  const send = async () => {
    try {
      await post(`/ideas/${ideaId}/urgent`, { note }); haptic.ok(); setOpen(false); setNote('')
      toast('Отправил. Буду напоминать, пока не ответит'); emit('home'); onDone()
    } catch (e) { toast(humanError(e)) }
  }
  return <>
    <div data-testid="ask-urgent"><Btn kind="soft" block icon="flame" onClick={() => setOpen(true)}>Важно — пусть {partner} ответит</Btn></div>
    <Sheet open={open} onClose={() => setOpen(false)}>
      <div className="col">
        <h2 className="title">Попросить ответить</h2>
        <p className="callout muted" style={{ margin: 0 }}>{partner} получит сообщение с кнопками «Идём», «Не смогу», «Отвечу через час». Пока не ответит — я напоминаю: через час, два, три… ночью молчу.</p>
        <div className="field"><label>Пара слов (необязательно)</label>
          <input className="input" value={note} maxLength={200} placeholder="Билеты быстро разбирают!" onChange={e => setNote(e.target.value)} /></div>
        <Btn kind="accent" block shine icon="flame" onClick={send}>Отправить</Btn>
      </div>
    </Sheet>
  </>
}

export async function answerUrgent(id: number, a: 'yes' | 'no') {
  try {
    const r = await post<{ text: string; round_id?: number }>(`/ideas/${id}/urgent/${a}`)
    haptic.ok(); toast(r.text); emit('home'); emit('ideas')
    if (a === 'yes' && r.round_id) go('round/' + r.round_id)
    return true
  } catch (e) { toast(humanError(e)); return false }
}

// on an idea: either «ждём ответа» (mine) or the two answers (theirs)
export function UrgentBanner({ ideaId, u, partner, onChange }: { ideaId: number; u: UrgentInfo; partner: string; onChange: () => void }) {
  if (!u) return null
  if (u.by_me) return (
    <div className="urgent rise" data-testid="urgent-mine">
      <Icon n="flame" s={20} /><div className="urgent__main"><b>Ждём ответа {ncase(partner, 'gen')}</b>
        <span>{u.nudges ? `Напомнил ${u.nudges} ${u.nudges === 1 ? 'раз' : u.nudges < 5 ? 'раза' : 'раз'}` : 'Сообщение отправлено'}</span></div>
      <button className="link" onClick={async () => { try { await del(`/ideas/${ideaId}/urgent`); onChange() } catch (e) { toast(humanError(e)) } }}>Отменить</button>
    </div>)
  return (
    <div className="urgent urgent--ask rise" data-testid="urgent-ask">
      <div className="row" style={{ gap: 10 }}><Icon n="flame" s={20} /><div className="urgent__main"><b>{partner} просит ответить</b>{u.note && <span>«{u.note}»</span>}</div></div>
      <div className="row" style={{ gap: 8 }}>
        <Btn kind="accent" sm onClick={async () => { if (await answerUrgent(ideaId, 'yes')) onChange() }}>Идём</Btn>
        <Btn kind="ghost" sm onClick={async () => { if (await answerUrgent(ideaId, 'no')) onChange() }}>Не смогу</Btn>
      </div>
    </div>)
}

// Home: everything waiting for my answer, on top
export function UrgentList({ items, partner }: { items: UrgentAsk[]; partner: string }) {
  if (!items?.length) return null
  return (
    <div className="col" style={{ gap: 8 }} data-testid="urgent-home">
      {items.map(u => (
        <div key={u.idea_id} className="urgent urgent--ask rise">
          <div className="row" style={{ gap: 10 }} onClick={() => go('idea/' + u.idea_id)}><Icon n="flame" s={20} />
            <div className="urgent__main"><b>{partner} просит ответить: {u.title}</b><span>{[u.when, u.note && `«${u.note}»`].filter(Boolean).join(' · ')}</span></div></div>
          <div className="row" style={{ gap: 8 }}>
            <Btn kind="accent" sm onClick={async () => { await answerUrgent(u.idea_id, 'yes') }}>{u.fixed ? 'Идём! В календарь' : 'Идём — выбрать время'}</Btn>
            <Btn kind="ghost" sm onClick={async () => { await answerUrgent(u.idea_id, 'no') }}>Не смогу</Btn>
          </div>
        </div>))}
    </div>)
}
