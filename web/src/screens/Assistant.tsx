import { useEffect, useMemo, useRef, useState } from 'react'
import type { ActionBtn, ChatMsg, Home, Reply } from '../lib/types'
import { useLoad, emit, patch } from '../lib/data'
import { post, del, get, upload, humanError } from '../lib/api'
import { complete } from '../lib/complete'
import { go } from '../lib/router'
import { haptic, openLink } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Cover, toast } from '../ui/kit'
import { Particles } from '../ui/Particles'

const SUGG = ['Куда сходить в субботу вечером?', 'Удиви нас', 'Что нашёл скаут?', 'Обо мне — настроить вкусы']
// after a search when the server sent no follow-ups (older answers): refine it in one tap
const FOLLOW = ['А подешевле?', 'А в воскресенье?', 'А спокойнее?', 'А без театра?']
const canRecord = () => typeof window !== 'undefined' && !!navigator.mediaDevices?.getUserMedia && typeof MediaRecorder !== 'undefined'

// a question in flight lives outside the screen: switch tabs while it thinks — the answer still arrives
// and is waiting in the chat when you come back
type Chat = { messages: ChatMsg[]; ai: boolean; voice?: boolean }
let inflight: Promise<unknown> | null = null
const addMsg = (m: ChatMsg) => patch<Chat>('/assistant', d => ({ ...(d || { ai: false }), messages: [...(d?.messages || []), m] }))

export function Assistant({ home }: { home: Home | null }) {
  const { data, set } = useLoad<{ messages: ChatMsg[]; ai: boolean; voice?: boolean }>('/assistant')
  const [text, setTextRaw] = useState(() => { try { return localStorage.getItem('vd-draft') || '' } catch { return '' } })
  const setText = (t: string) => { setTextRaw(t); try { t ? localStorage.setItem('vd-draft', t) : localStorage.removeItem('vd-draft') } catch { /* private mode */ } }
  const [failed, setFailed] = useState<Record<number, string>>({}) // my message id → its text, to retry
  const [atBottom, setAtBottom] = useState(true)
  const first = useRef(true)
  const [hints, setHints] = useState<{ phrases: string[]; titles: string[] }>({ phrases: [], titles: [] })
  const [rec, setRec] = useState<{ r: MediaRecorder; started: number } | null>(null)
  const [secs, setSecs] = useState(0)
  const input = useRef<HTMLTextAreaElement>(null)
  useEffect(() => { get<{ phrases: string[]; titles: string[] }>('/assistant/hints').then(setHints).catch(() => {}) }, [])
  const typed = useMemo(() => complete(text, hints.phrases, hints.titles), [text, hints])
  useEffect(() => { if (!rec) return; const t = setInterval(() => setSecs(Math.round((Date.now() - rec.started) / 1000)), 250); return () => clearInterval(t) }, [rec])
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState<Record<string, string>>({})
  const end = useRef<HTMLDivElement>(null)
  const msgs = data?.messages || []
  const lastAi = [...msgs].reverse().find(m => m.role === 'assistant')
  const lastPlan = !!lastAi?.reply.actions?.some(a => a.kind === 'plan' || a.kind === 'book')
  // like a messenger: open at the last message without a scroll show; follow new ones only when already at the bottom
  useEffect(() => {
    if (!msgs.length) return
    if (first.current) { first.current = false; end.current?.scrollIntoView({ block: 'end' }); return }
    if (atBottom) end.current?.scrollIntoView({ behavior: 'smooth', block: 'end' })
  }, [msgs.length, busy])
  useEffect(() => {
    const on = () => { const el = document.scrollingElement || document.documentElement; setAtBottom(el.scrollHeight - el.scrollTop - window.innerHeight < 160) }
    window.addEventListener('scroll', on, { passive: true }); return () => window.removeEventListener('scroll', on)
  }, [])
  // the keyboard: the composer rides on top of it, the tab bar steps aside
  useEffect(() => {
    const vv = window.visualViewport; if (!vv) return
    const on = () => { const kb = Math.max(0, window.innerHeight - vv.height - vv.offsetTop); document.documentElement.style.setProperty('--kb', kb + 'px'); document.body.classList.toggle('kb-open', kb > 80) }
    vv.addEventListener('resize', on); vv.addEventListener('scroll', on); on()
    return () => { vv.removeEventListener('resize', on); vv.removeEventListener('scroll', on); document.body.classList.remove('kb-open'); document.documentElement.style.removeProperty('--kb') }
  }, [])
  // the field grows with the text, up to five lines
  useEffect(() => { const el = input.current; if (!el) return; el.style.height = 'auto'; el.style.height = Math.min(el.scrollHeight, 5 * 21 + 20) + 'px' }, [text])

  const ask = async (q: string, retry?: number) => {
    q = q.trim()
    if (!q || busy) return
    haptic.tap(); if (!retry) setText('')
    const id = retry || Date.now()
    if (retry) setFailed(f => { const n = { ...f }; delete n[retry]; return n })
    else addMsg({ id, role: 'user', at: Date.now() / 1000, reply: { text: q } })
    setBusy(true); setAtBottom(true)
    const p = post<{ reply: Reply }>('/assistant', { text: q }).then(r => { say(r.reply); haptic.soft() })
    inflight = p.finally(() => { if (inflight === p2) inflight = null })
    const p2 = inflight
    try { await p } catch (e) { setFailed(f => ({ ...f, [id]: q })); toast(humanError(e)) } finally { setBusy(false) }
  }
  const say = (r: Reply) => addMsg({ id: Date.now() + 1, role: 'assistant', at: Date.now() / 1000, reply: r })
  // came back while an answer is on its way: show «печатает…» until it lands
  useEffect(() => { if (inflight) { setBusy(true); inflight.catch(() => {}).finally(() => setBusy(false)) } }, [])
  const press = async (a: ActionBtn) => {
    if (a.kind === 'open' && a.path) { haptic.soft(); go(a.path); return } // just a way to a screen
    if (done[a.id] || busy) return
    if (a.kind === 'clarify') setBusy(true) // the other reading answers as a new message
    try {
      const r = await post<{ reply: Reply }>('/assistant/act', { id: a.id })
      if (r.reply.open !== undefined) { go(r.reply.open); return }
      setDone(d => ({ ...d, [a.id]: r.reply.text })); haptic.ok()
      if (a.kind === 'clarify') say(r.reply)
      else { emit('home'); emit('ideas'); toast(r.reply.text) }
    } catch (e) { toast(humanError(e)) } finally { setBusy(false) }
  }
  // a phrase that wants a name («Сколько стоит ») goes into the field; the rest is asked right away
  const pick = (s: string) => { if (s.endsWith(' ')) { setText(s); input.current?.focus() } else ask(s) }

  const record = async () => {
    if (rec) { rec.r.stop(); return }
    let stream: MediaStream
    try { stream = await navigator.mediaDevices.getUserMedia({ audio: true }) } catch { toast('Нет доступа к микрофону'); return }
    const r = new MediaRecorder(stream)
    const parts: Blob[] = []
    r.ondataavailable = e => e.data.size && parts.push(e.data)
    r.onstop = async () => {
      stream.getTracks().forEach(t => t.stop()); setRec(null); setSecs(0)
      const blob = new Blob(parts, { type: r.mimeType || 'audio/webm' })
      if (blob.size < 2000) return // a tap, not speech
      const ext = /mp4|aac|m4a/.test(blob.type) ? 'mp4' : /ogg/.test(blob.type) ? 'ogg' : 'webm'
      const f = new FormData(); f.append('audio', blob, 'rec.' + ext)
      setBusy(true)
      try {
        const res = await upload<{ heard: string; reply?: Reply }>('/assistant/voice', f)
        if (!res.heard || !res.reply) { toast('Не расслышал — повтори или напиши'); return }
        set(d => ({ ai: d?.ai ?? false, voice: d?.voice, messages: [...(d?.messages || []), { id: Date.now(), role: 'user', at: Date.now() / 1000, reply: { text: res.heard } }] }))
        say(res.reply); haptic.soft()
      } catch (e) { toast(humanError(e)) } finally { setBusy(false) }
    }
    r.start(); haptic.tap(); setRec({ r, started: Date.now() })
    setTimeout(() => r.state === 'recording' && r.stop(), 60000)
  }
  const clear = async () => { await del('/assistant'); set(d => ({ ...d, ai: d?.ai ?? false, messages: [] })) }
  const next = lastAi?.reply.next?.length ? lastAi.reply.next : lastAi?.reply.intent?.i === 'find' ? FOLLOW : []
  const voice = !!data?.voice && canRecord()

  return (
    <>
      <header className="hdr"><span className="mini-orb"><Particles shape={busy ? 'ring' : lastPlan ? 'heart' : 'sphere'} count={700} scale={0.3} interactive={false} /></span><h1 className="display">Помощник</h1><span className="spacer" />
        {msgs.length > 0 && <button className="iconbtn" onClick={clear} aria-label="Очистить"><Icon n="trash" s={19} /></button>}</header>
      <div className="chat">
        {msgs.length === 0 && (
          <div className="rise" style={{ textAlign: 'center' }}>
            <div className="orb-canvas"><Particles shape={busy ? 'ring' : 'sphere'} count={2200} scale={0.34} /></div>
            <h2 className="title">Чем помочь?</h2>
            <p className="callout muted" style={{ margin: '6px auto 18px', maxWidth: 300 }}>Подберу, куда сходить, найду идею под бюджет, напомню, что запланировано. Решаете — вы, я только считаю и подсказываю.</p>
          </div>
        )}
        {msgs.map((m, k) => {
          const day = dayOf(m.at), prev = k ? dayOf(msgs[k - 1].at) : ''
          return <div key={m.id} className="col" style={{ gap: 4 }}>
            {day !== prev && <div className="chat__day">{dayLabel(m.at)}</div>}
            {m.role === 'user'
              ? <div className="msg msg--me" data-failed={failed[m.id] ? '' : undefined}>{m.reply.text}<span className="msg__time">{hm(m.at)}{failed[m.id] ? ' · не отправлено' : busy && k === msgs.length - 1 ? ' · ⏱' : ' ✓'}</span></div>
              : <AiMsg r={m.reply} at={m.at} done={done} onPress={press} />}
            {failed[m.id] && <button className="msg__retry" onClick={() => ask(failed[m.id], m.id)}><Icon n="spark" s={14} />Повторить</button>}
          </div>
        })}
        {busy && <div className="msg msg--ai typing" aria-label="Печатает"><i /><i /><i /></div>}
        <div ref={end} style={{ scrollMarginBottom: "calc(var(--dock-h) + var(--safe-b) + 90px)" }} /* stop above the composer and the tab bar */ />
      </div>
      {!atBottom && msgs.length > 3 && <button className="chat__down" aria-label="К последнему сообщению" onClick={() => { setAtBottom(true); end.current?.scrollIntoView({ behavior: 'smooth', block: 'end' }) }}><Icon n="chev" s={18} w={2.4} /></button>}
      {text.trim() && typed.length > 0 && !busy
        ? <div className="sugg sugg--dock" data-testid="type-hints">{typed.map(s => <button key={s} onClick={() => pick(s)}>{s.trim()}</button>)}</div>
        : !text.trim() && msgs.length >= 2 && !busy && next.length > 0 && lastAi === msgs[msgs.length - 1]
          ? <div className="sugg sugg--dock rise" data-testid="follow">{next.map(s => <button key={s} onClick={() => ask(s)}>{s}</button>)}</div>
          : !text.trim() && msgs.length < 2 && !busy && <div className="sugg sugg--dock rise" style={{ ['--d' as any]: 2 }}>{SUGG.map(s => <button key={s} onClick={() => ask(s)}>{s}</button>)}</div>}
      <form className="composer" data-rec={rec ? '' : undefined} onSubmit={e => { e.preventDefault(); ask(text) }}>
        {rec
          ? <div className="composer__rec" aria-live="polite"><i />Слушаю… {secs} с</div>
          : <textarea ref={input} rows={1} className="input composer__field" placeholder={home?.paired ? 'Спроси про ваши планы…' : 'Спроси что-нибудь…'} value={text} maxLength={600}
              onChange={e => setText(e.target.value)} enterKeyHint="send"
              onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); ask(text) } }} />}
        {voice && !text.trim()
          ? <button className="send" type="button" data-testid="mic" onClick={record} disabled={busy} aria-label={rec ? 'Остановить' : 'Сказать голосом'}><Icon n={rec ? 'stop' : 'mic'} s={20} w={2.2} /></button>
          : <button className="send" type="submit" disabled={!text.trim() || busy} aria-label="Отправить" onPointerDown={e => e.preventDefault() /* keep the keyboard open, like a messenger */}><Icon n="send" s={20} w={2.4} /></button>}
      </form>
    </>
  )
}

const pad = (n: number) => String(n).padStart(2, '0')
const hm = (t: number) => { const d = new Date(t * 1000); return pad(d.getHours()) + ':' + pad(d.getMinutes()) }
const dayOf = (t: number) => new Date(t * 1000).toDateString()
function dayLabel(t: number) {
  const d = new Date(t * 1000), now = new Date(), y = new Date(Date.now() - 86400_000)
  if (d.toDateString() === now.toDateString()) return 'Сегодня'
  if (d.toDateString() === y.toDateString()) return 'Вчера'
  return d.toLocaleDateString('ru', { day: 'numeric', month: 'long', ...(d.getFullYear() !== now.getFullYear() ? { year: 'numeric' } : {}) })
}

function AiMsg({ r, at, done, onPress }: { r: Reply; at: number; done: Record<string, string>; onPress: (a: ActionBtn) => void }) {
  return (
    <div className="col" style={{ gap: 8 }}>
      <div className="msg msg--ai">{linkify(r.text)}<span className="msg__time">{hm(at)}</span></div>
      {!!r.cards?.length && (
        <div className="ai-cards">
          {r.cards.map((c, k) => (
            <div key={k} className="ai-card rise" style={{ ['--d' as any]: k }} onClick={() => go(c.type === 'meeting' && c.round_id ? 'round/' + c.round_id : 'idea/' + c.idea_id)}>
              <Cover idea={{ image: c.image, category: c.category || 'other', title: c.title }}>
                <span>{c.when && <span className="pill pill--glass"><Icon n="cal" s={12} w={2.2} />{c.when}</span>}</span>
                <div className="cover__title" style={{ fontSize: 15, lineHeight: '19px' }}>{c.title}</div>
              </Cover>
              <div style={{ padding: '8px 4px 2px' }}>
                {c.meta && <p className="caption muted" style={{ margin: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{c.meta}</p>}
                {c.about && <p className="caption" style={{ margin: '4px 0 0', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>{c.about}</p>}
                {c.why && <p className="caption muted" style={{ margin: '4px 0 0' }}>{c.why}</p>}
                <span className="caption" style={{ display: 'inline-block', marginTop: 6, color: 'var(--accent-deep, var(--accent))', fontWeight: 600 }}>Подробнее ›</span>
              </div>
            </div>
          ))}
        </div>
      )}
      {r.pick && <button className="pickcard rise" data-testid="pick-open" onClick={() => go('pick/' + r.pick!.id)}>
        <span className="pickcard__ico"><Icon n="folder" s={20} /></span>
        <span className="pickcard__main"><b>Все {r.pick.n} — списком</b><small>{r.pick.title}</small></span>
        <Icon n="chev" s={16} w={2.4} /></button>}
      {!!r.actions?.length && (
        <div className="ai-acts">
          {r.actions.map(a => (
            <button key={a.id} className={'ai-act' + (a.kind === 'clarify' ? ' ai-act--alt' : '')} data-done={done[a.id] ? '' : undefined} data-kind={a.kind} onClick={() => onPress(a)}>
              <Icon n={done[a.id] ? 'check' : a.kind === 'plan' || a.kind === 'book' ? 'cal' : a.kind === 'add_idea' ? 'plus' : a.kind === 'clarify' ? 'spark' : 'spark'} s={16} w={2.4} />{done[a.id] ? (a.kind === 'clarify' ? 'Ответил ниже' : 'Готово') : a.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// links in answers (tickets, «посмотреть самому») open inside Telegram
function linkify(t: string) {
  const parts = t.split(/(https?:\/\/[^\s)]+)/g)
  return parts.map((p, k) => /^https?:\/\//.test(p)
    ? <a key={k} href={p} onClick={e => { e.preventDefault(); openLink(p) }} className="msg__link">{linkLabel(p)}</a>
    : p)
}
function linkLabel(u: string) {
  try { const h = new URL(u).hostname.replace(/^www\./, ''); return /yandex\.ru\/search/.test(u) ? 'поиск в Яндексе' : h } catch { return u }
}
