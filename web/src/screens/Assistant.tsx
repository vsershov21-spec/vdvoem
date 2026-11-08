import { useEffect, useRef, useState } from 'react'
import type { ChatMsg, Home, Reply } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, del, humanError } from '../lib/api'
import { go } from '../lib/router'
import { haptic, openLink } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Cover, toast } from '../ui/kit'
import { Particles } from '../ui/Particles'

const SUGG = ['Куда сходить в субботу вечером?', 'Хотим почилить', 'Удиви нас', 'Что нашёл скаут?', 'Что у нас запланировано?', 'Анонсы на пару месяцев', 'Что-нибудь до 3000 ₽']
// after a search: refine it in one tap (the server keeps the rest of the request)
const FOLLOW = ['А подешевле?', 'А в воскресенье?', 'А спокойнее?', 'А без театра?']

export function Assistant({ home }: { home: Home | null }) {
  const { data, set } = useLoad<{ messages: ChatMsg[]; ai: boolean }>('/assistant')
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState<Record<string, string>>({})
  const end = useRef<HTMLDivElement>(null)
  const msgs = data?.messages || []
  const lastAi = [...msgs].reverse().find(m => m.role === 'assistant')
  const lastPlan = !!lastAi?.reply.actions?.some(a => a.kind === 'plan' || a.kind === 'book')
  useEffect(() => { end.current?.scrollIntoView({ behavior: 'smooth', block: 'end' }) }, [msgs.length, busy])

  const ask = async (q: string) => {
    q = q.trim()
    if (!q || busy) return
    haptic.tap(); setText('')
    const mine: ChatMsg = { id: Date.now(), role: 'user', at: Date.now() / 1000, reply: { text: q } }
    set(d => ({ ai: d?.ai ?? false, messages: [...(d?.messages || []), mine] }))
    setBusy(true)
    try {
      const r = await post<{ reply: Reply }>('/assistant', { text: q })
      set(d => ({ ai: d?.ai ?? false, messages: [...(d?.messages || []), { id: Date.now() + 1, role: 'assistant', at: Date.now() / 1000, reply: r.reply }] }))
      haptic.soft()
    } catch (e) { toast(humanError(e)) } finally { setBusy(false) }
  }
  const press = async (id: string) => {
    if (done[id]) return
    try {
      const r = await post<{ reply: Reply }>('/assistant/act', { id })
      setDone(d => ({ ...d, [id]: r.reply.text })); haptic.ok(); emit('home'); emit('ideas')
      toast(r.reply.text)
    } catch (e) { toast(humanError(e)) }
  }
  const clear = async () => { await del('/assistant'); set(d => ({ ai: d?.ai ?? false, messages: [] })) }

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
        {msgs.map(m => m.role === 'user'
          ? <div key={m.id} className="msg msg--me">{m.reply.text}</div>
          : <AiMsg key={m.id} r={m.reply} done={done} onPress={press} />)}
        {busy && <div className="msg msg--ai typing" aria-label="Печатает"><i /><i /><i /></div>}
        <div ref={end} style={{ scrollMarginBottom: "calc(var(--dock-h) + var(--safe-b) + 90px)" }} /* stop above the composer and the tab bar */ />
      </div>
      {msgs.length >= 2 && !busy && lastAi?.reply.intent?.i === 'find' && lastAi === msgs[msgs.length - 1] &&
        <div className="sugg sugg--dock rise" data-testid="follow">{FOLLOW.map(s => <button key={s} onClick={() => ask(s)}>{s}</button>)}</div>}
      {msgs.length < 2 && !busy && <div className="sugg sugg--dock rise" style={{ ['--d' as any]: 2 }}>{SUGG.map(s => <button key={s} onClick={() => ask(s)}>{s}</button>)}</div>}
      <form className="composer" onSubmit={e => { e.preventDefault(); ask(text) }}>
        <input className="input" placeholder={home?.paired ? 'Спроси про ваши планы…' : 'Спроси что-нибудь…'} value={text} maxLength={600} onChange={e => setText(e.target.value)} />
        <button className="send" type="submit" disabled={!text.trim() || busy} aria-label="Отправить"><Icon n="send" s={20} w={2.4} /></button>
      </form>
    </>
  )
}

function AiMsg({ r, done, onPress }: { r: Reply; done: Record<string, string>; onPress: (id: string) => void }) {
  return (
    <div className="col" style={{ gap: 8 }}>
      <div className="msg msg--ai">{linkify(r.text)}</div>
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
                {c.why && <p className="caption" style={{ margin: '4px 0 0' }}>{c.why}</p>}
              </div>
            </div>
          ))}
        </div>
      )}
      {!!r.actions?.length && (
        <div className="ai-acts">
          {r.actions.map(a => (
            <button key={a.id} className="ai-act" data-done={done[a.id] ? '' : undefined} onClick={() => onPress(a.id)}>
              <Icon n={done[a.id] ? 'check' : a.kind === 'plan' || a.kind === 'book' ? 'cal' : a.kind === 'add_idea' ? 'plus' : 'spark'} s={16} w={2.4} />{done[a.id] ? 'Готово' : a.label}
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
