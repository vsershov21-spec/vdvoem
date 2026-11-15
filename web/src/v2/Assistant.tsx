import { useEffect, useRef, useState } from 'react'
import { useLoad } from '../lib/data'
import { get, post, del, upload, humanError, ApiError } from '../lib/api'
import { go } from '../lib/router'
import { haptic, openLink } from '../lib/tg'
import type { ActionBtn, Card, ChatMsg, Reply } from '../lib/types'
import { Pic, say } from './ui'

const canRecord = () => typeof navigator !== 'undefined' && !!navigator.mediaDevices?.getUserMedia && typeof MediaRecorder !== 'undefined'
const VOICE_ERR: Record<string, string> = { voice_failed: 'Не получилось разобрать голос. Напиши текстом.', audio_too_long: 'Слишком длинно. Скажи покороче.', voice_off: 'Голос сейчас выключен. Напиши текстом.', bad_audio: 'Запись не дошла. Попробуй ещё раз.' }
const MIC = '<rect x="9" y="3.500" width="6" height="11" rx="3"/><path d="M5.500 11.500a6.500 6.500 0 0 0 13 0M12 18v3"/>'

// The assistant: a chat over the couple's ideas and the afisha. What is in the data comes back as cards; what
// is not — an honest «нет», a link to look now, and a question to the scout whose answer comes here and to the bot.
export function Assistant() {
  const { data, set, refresh } = useLoad<{ messages: ChatMsg[]; ai: boolean; voice?: boolean }>('/assistant')
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [hints, setHints] = useState<string[]>([])
  const [pending, setPending] = useState('')
  // voice: the same road as in the bot. «on» is recording, «wait» is the server turning speech into text
  const [rec, setRec] = useState<'' | 'on' | 'wait'>('')
  const [secs, setSecs] = useState(0)
  const [verr, setVerr] = useState('')
  const live = useRef<{ r: MediaRecorder; stream: MediaStream; drop: boolean } | null>(null)
  useEffect(() => () => { const x = live.current; if (x) { x.drop = true; try { x.r.stop() } catch { /* already stopped */ } x.stream.getTracks().forEach(t => t.stop()) } }, [])
  useEffect(() => { if (rec !== 'on') return; const t0 = Date.now(), t = setInterval(() => setSecs(Math.round((Date.now() - t0) / 1000)), 250); return () => clearInterval(t) }, [rec])
  const log = useRef<HTMLDivElement>(null)
  useEffect(() => { get<{ phrases: string[] }>('/assistant/hints').then(h => setHints(h.phrases || [])).catch(() => {}) }, [])
  const msgs = data?.messages || []
  // the newest is at the bottom, right above the field: the list scrolls, the field stays where it is
  useEffect(() => { const el = log.current; if (el) el.scrollTop = el.scrollHeight }, [msgs.length, pending, rec])
  const send = async (q: string) => {
    q = q.trim(); if (!q || busy) return
    setText(''); setPending(q); setBusy(true); setVerr('')
    try { await post<{ reply: Reply }>('/assistant', { text: q }); haptic.soft(); await refresh() }
    catch (e) { say(humanError(e)) } finally { setPending(''); setBusy(false) }
  }
  const record = async () => {
    if (rec === 'on') { live.current?.r.stop(); return }
    if (busy || rec) return
    setVerr('')
    let stream: MediaStream
    try { stream = await navigator.mediaDevices.getUserMedia({ audio: true }) }
    catch (e) { setVerr((e as { name?: string })?.name === 'NotFoundError' ? 'Микрофон не найден. Напиши текстом.' : 'Нет доступа к микрофону. Разреши его в настройках телефона или напиши текстом.'); return }
    let r: MediaRecorder
    try { r = new MediaRecorder(stream) } catch { stream.getTracks().forEach(t => t.stop()); setVerr('Запись здесь не работает. Напиши текстом.'); return }
    const parts: Blob[] = [], me = { r, stream, drop: false }
    live.current = me
    r.ondataavailable = e => { if (e.data.size) parts.push(e.data) }
    r.onstop = async () => {
      stream.getTracks().forEach(t => t.stop()); live.current = null
      if (me.drop) return
      const blob = new Blob(parts, { type: r.mimeType || 'audio/webm' })
      if (blob.size < 2000) { setRec(''); setVerr('Ничего не записалось. Нажми, скажи и нажми ещё раз.'); return } // a tap, not speech
      const ext = /mp4|aac|m4a/.test(blob.type) ? 'mp4' : /ogg/.test(blob.type) ? 'ogg' : 'webm'
      const f = new FormData(); f.append('audio', blob, 'rec.' + ext)
      setRec('wait'); setBusy(true)
      try {
        const res = await upload<{ heard: string; reply?: Reply }>('/assistant/voice', f)
        if (!res.heard || !res.reply) setVerr('Не расслышал. Повтори или напиши текстом.')
        else { haptic.soft(); await refresh() }
      } catch (e) { setVerr((e instanceof ApiError && VOICE_ERR[e.code]) || humanError(e)) } finally { setRec(''); setBusy(false) }
    }
    r.start(); haptic.tap?.(); setSecs(0); setRec('on')
    setTimeout(() => { if (r.state === 'recording') r.stop() }, 60000) // a minute is the server's limit
  }
  const voice = !!data?.voice && canRecord()
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
    <div className="scr v-chat" data-testid="v2-assistant">
      <div className="v-row"><h2>Помощник</h2><span className="sp" />{msgs.length > 0 && <button className="back" onClick={async () => { await del('/assistant'); set(d => ({ ai: d?.ai ?? false, messages: [] })) }}>Очистить</button>}</div>
      <div className="v-log" ref={log} data-testid="chat-log">
      {data && !msgs.length && !pending && <>
        <p className="m">Ищу по вашим идеям и афише. Чего нет, спрошу у скаута: он ищет события в сети и отвечает сюда и в бот.</p>
        <div className="v-chips" style={{ flexWrap: 'wrap', overflow: 'visible' }}>{(hints.length ? hints.slice(0, 4) : ['Что в кино на выходных?', 'Куда поужинать?']).map(h => <button className="v-chip" key={h} onClick={() => send(h)}>{h}</button>)}</div>
      </>}
      {msgs.map(m => m.role === 'user'
        ? <div className="v-msg me" key={m.id}>{m.reply.text}</div>
        : <div key={m.id} style={{ display: 'flex', flexDirection: 'column', gap: 8 }} data-testid="chat-reply">
          {m.reply.text && <div className="v-msg">{m.reply.text}</div>}
          {m.reply.cards?.map(card)}
          {m.reply.pick && <button className="in" data-testid="pick-open" onClick={() => go('pick/' + m.reply.pick!.id)}><i>☰</i><span><b>Все {m.reply.pick.n} списком</b><small>{m.reply.pick.title}</small></span>›</button>}
          {m.reply.actions && m.reply.actions.length > 0 && <div className="v-row" style={{ flexWrap: 'wrap' }}>{m.reply.actions.map(a => <button key={a.id} className="b s" disabled={busy} onClick={() => press(a)}>{a.label}{a.kind === 'link' ? ' ↗' : ''}</button>)}</div>}
        </div>)}
      {pending && <><div className="v-msg me">{pending}</div><div className="v-msg" data-testid="chat-wait">Думаю…</div></>}
      {!pending && last?.role === 'assistant' && last.reply.next && last.reply.next.length > 0 && <div className="v-chips" style={{ flexWrap: 'wrap', overflow: 'visible' }}>{last.reply.next.slice(0, 3).map(h => <button className="v-chip" key={h} onClick={() => send(h)}>{h}</button>)}</div>}
      </div>
      {verr && <p className="m red" role="alert" data-testid="voice-err">{verr}</p>}
      <div className="v-composer" data-rec={rec || undefined}>
        {rec === 'on' ? <span className="v-rec" role="status" data-testid="voice-state"><i />Слушаю… {secs} с</span>
          : rec === 'wait' ? <span className="v-rec" role="status" data-testid="voice-state">Распознаю…</span>
            : <input value={text} onChange={e => setText(e.target.value)} placeholder={voice ? 'Спроси или скажи' : 'Спроси про идеи, афишу, планы'} aria-label="Вопрос помощнику" data-testid="chat-input" onKeyDown={e => { if (e.key === 'Enter') send(text) }} />}
        {voice && <button className={'b s v-mic' + (rec === 'on' ? ' on' : '')} aria-label={rec === 'on' ? 'Остановить запись' : 'Сказать голосом'} aria-pressed={rec === 'on'} disabled={rec === 'wait' || (busy && rec !== 'on')} data-testid="chat-mic" onClick={record}>
          {rec === 'on' ? '■' : <svg viewBox="0 0 24 24" aria-hidden="true" dangerouslySetInnerHTML={{ __html: MIC }} />}</button>}
        {rec !== 'on' && <button className="b a s" aria-label="Отправить" disabled={busy || !text.trim()} data-testid="chat-send" onClick={() => send(text)}>↑</button>}
      </div>
    </div>)
}
