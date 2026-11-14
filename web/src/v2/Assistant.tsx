import { useEffect, useRef, useState } from 'react'
import { useLoad } from '../lib/data'
import { get, post, del, humanError } from '../lib/api'
import { go } from '../lib/router'
import { haptic, openLink } from '../lib/tg'
import type { ActionBtn, Card, ChatMsg, Reply } from '../lib/types'
import { Pic, say } from './ui'

// The assistant: a chat over the couple's ideas and the afisha. What is in the data comes back as cards; what
// is not — an honest «нет», a link to look now, and a question to the scout whose answer comes here and to the bot.
export function Assistant() {
  const { data, set, refresh } = useLoad<{ messages: ChatMsg[]; ai: boolean }>('/assistant')
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [hints, setHints] = useState<string[]>([])
  const [pending, setPending] = useState('')
  const end = useRef<HTMLDivElement>(null)
  useEffect(() => { get<{ phrases: string[] }>('/assistant/hints').then(h => setHints(h.phrases || [])).catch(() => {}) }, [])
  const msgs = data?.messages || []
  useEffect(() => { end.current?.scrollIntoView({ block: 'end' }) }, [msgs.length, pending])
  const send = async (q: string) => {
    q = q.trim(); if (!q || busy) return
    setText(''); setPending(q); setBusy(true)
    try { await post<{ reply: Reply }>('/assistant', { text: q }); haptic.soft(); await refresh() }
    catch (e) { say(humanError(e)) } finally { setPending(''); setBusy(false) }
  }
  const press = async (a: ActionBtn) => {
    const way = a.kind === 'open' ? (a.path || '').replace(/^round\//, 'meet/').replace(/^calendar$/, 'plans').replace(/^ideas\/.*/, 'ideas') : ''
    if (way) { go(way); return }
    if (a.kind === 'link' && a.url) { openLink(a.url); return }
    setBusy(true)
    try { await post('/assistant/act', { id: a.id }); haptic.ok(); await refresh() } catch (e) { say(humanError(e)) } finally { setBusy(false) }
  }
  const card = (c: Card, k: number) => (
    <button className="in" key={k} data-testid="chat-card" onClick={() => go(c.type === 'meeting' && c.round_id ? 'meet/' + c.round_id : 'idea/' + c.idea_id)}>
      <Pic idea={{ title: c.title, category: c.category, image: c.image }} className="th" /><span><b>{c.title}</b><small>{[c.when, c.meta].filter(Boolean).join(' · ')}</small></span>›</button>)
  const last = msgs[msgs.length - 1]
  return (
    <div className="scr" data-testid="v2-assistant">
      <div className="v-row"><h2>Помощник</h2><span className="sp" />{msgs.length > 0 && <button className="back" onClick={async () => { await del('/assistant'); set(d => ({ ai: d?.ai ?? false, messages: [] })) }}>Очистить</button>}</div>
      {data && !msgs.length && !pending && <>
        <p className="m">Ищу по вашим идеям и афише. Чего нет, спрошу у скаута: он ищет события в сети и отвечает сюда и в бот.</p>
        <div className="v-chips" style={{ flexWrap: 'wrap' }}>{(hints.length ? hints.slice(0, 4) : ['Что в кино на выходных?', 'Куда поужинать?']).map(h => <button className="v-chip" key={h} onClick={() => send(h)}>{h}</button>)}</div>
      </>}
      {msgs.map(m => m.role === 'user'
        ? <div className="v-msg me" key={m.id}>{m.reply.text}</div>
        : <div key={m.id} style={{ display: 'flex', flexDirection: 'column', gap: 8 }} data-testid="chat-reply">
          {m.reply.text && <div className="v-msg">{m.reply.text}</div>}
          {m.reply.cards?.map(card)}
          {m.reply.actions && m.reply.actions.length > 0 && <div className="v-row" style={{ flexWrap: 'wrap' }}>{m.reply.actions.map(a => <button key={a.id} className="b s" disabled={busy} onClick={() => press(a)}>{a.label}{a.kind === 'link' ? ' ↗' : ''}</button>)}</div>}
        </div>)}
      {pending && <><div className="v-msg me">{pending}</div><div className="v-msg" data-testid="chat-wait">Думаю…</div></>}
      {!pending && last?.role === 'assistant' && last.reply.next && last.reply.next.length > 0 && <div className="v-chips" style={{ flexWrap: 'wrap' }}>{last.reply.next.slice(0, 3).map(h => <button className="v-chip" key={h} onClick={() => send(h)}>{h}</button>)}</div>}
      <div ref={end} />
      <div className="composer"><input value={text} onChange={e => setText(e.target.value)} placeholder="Спроси про идеи, афишу, планы" aria-label="Вопрос помощнику" data-testid="chat-input" onKeyDown={e => { if (e.key === 'Enter') send(text) }} />
        <button className="b a s" aria-label="Отправить" disabled={busy || !text.trim()} data-testid="chat-send" onClick={() => send(text)}>↑</button></div>
    </div>)
}
