import type React from 'react'
import { useState } from 'react'
import type { Home as HomeT, Round } from '../lib/types'
import { go } from '../lib/router'
import { post, humanError } from '../lib/api'
import { useOnline } from '../lib/live'
import { relLeft, ncase, past } from '../lib/format'
import { shareLink } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Avatar, Btn, Cover, Skel, toast } from '../ui/kit'
import { answerUrgent, type UrgentAsk } from '../ui/Urgent'
import { Deck } from '../ui/Deck'
import { Going } from '../ui/Going'

// The main screen is the deck. Order by what matters: who we are → what waits for my answer (one slim row each,
// never a wall) → the deck (ours first, then the scout's afisha) → the assistant with ready questions.
export function Home({ home, onAdd, onSurprise, onStart }: { home: HomeT | null; onAdd: () => void; onSurprise: (m: 'random' | 'best') => void; onStart: () => void }) {
  const partnerHere = useOnline(home?.partner?.color)
  if (!home) return (<><Skel h={44} /><Skel h={52} /><Skel h={430} r={32} /></>)
  const hour = new Date().getHours()
  const hello = hour < 5 ? 'Доброй ночи' : hour < 12 ? 'Доброе утро' : hour < 18 ? 'Добрый день' : 'Добрый вечер'
  return (
    <>
      <header className="hdr" style={{ height: 'auto', margin: '4px 0 12px' }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <h1 className="title" style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{hello}, {home.me.name}</h1>
          <p className="caption muted" style={{ margin: '2px 0 0' }}>{home.partner ? (partnerHere ? <span className="here">{home.partner.name} сейчас здесь</span> : `Планируем вдвоём с ${ncase(home.partner.name, 'ins')}`) : 'Пригласи партнёра — и начнём'}</p>
        </div>
        <button className="iconbtn" aria-label="Добавить идею" onClick={onAdd}><Icon n="plus" s={20} w={2.3} /></button>
        <button className="pairava" style={{ border: 0, background: 'none', padding: 0, cursor: 'pointer' }} onClick={() => go('us')} aria-label="Мы">
          <Avatar p={home.me} /><Avatar p={home.partner} ghost={!home.partner} online={partnerHere} />
        </button>
      </header>
      {!home.paired && <Invite />}
      <Inbox home={home} />
      {home.paired && <>
        <Deck partner={home.partner} onAdd={onAdd} />
        {home.counts.both_want > 0 && <button className="matchlink" onClick={() => go('ideas/both')}><Icon n="heart" s={15} w={2.4} />Совпало у вас · {home.counts.both_want}<Icon n="chev" s={14} w={2.4} /></button>}
        <AskBar />
        <div className="row" style={{ gap: 8, marginTop: 10 }}>
          <Btn kind="surface" block icon="dice" onClick={() => onSurprise('random')}>Удиви нас</Btn>
          {!home.round && <Btn kind="surface" block icon="cal" onClick={onStart}>Выбрать вместе</Btn>}
        </div>
      </>}
      {!home.paired && <AskBar />}
      {home.taste_set === false && <TasteNudge />}
      {(home.going?.length || 0) > 1 && <Going list={home.going!} />}
    </>
  )
}

// the assistant on the main screen: one tap to ask, and ready questions people really ask
function AskBar() {
  const ask = (q: string) => { try { sessionStorage.setItem('vd-ask', q) } catch { /* private */ } go('assistant') }
  return (
    <div className="askbox rise">
      <button id="ask-bar" className="askbar__q" onClick={() => go('assistant')}><Icon n="spark" s={19} /><span>Спроси помощника или вставь ссылку</span></button>
      <div className="askbox__chips">
        {['Что в кино в выходные?', 'Куда сходить поужинать', 'Чем заняться сегодня вечером'].map(q => <button key={q} className="wkchip" onClick={() => ask(q)}>{q}</button>)}
      </div>
    </div>
  )
}

// «Ждёт тебя»: every open loop as one slim row — an answer the partner asked for, the choice in progress,
// tickets to buy, the next date. Two rows show; the rest fold under «ещё N».
function Inbox({ home }: { home: HomeT }) {
  const [all, setAll] = useState(false)
  const pn = home.partner?.name || 'Партнёр'
  const rows: React.ReactNode[] = []
  for (const u of home.urgent || []) rows.push(<UrgentRow key={'u' + u.idea_id} u={u} partner={pn} />)
  if (home.round) rows.push(<RoundRow key="r" r={home.round} home={home} />)
  for (const g of home.going || []) if (g.state === 'planned' && g.book && !g.tickets) rows.push(
    <button key={'t' + g.id} className="inrow" onClick={() => go('round/' + g.id)}>
      <span className="inrow__ic"><Icon n="wallet" s={17} /></span>
      <span className="inrow__main"><b>{g.book.label}</b><span>{[g.chosen?.title, g.when].filter(Boolean).join(' · ')}</span></span><Icon n="chev" s={15} w={2.2} />
    </button>)
  if (!home.round && home.next) rows.push(<NextCard key="n" r={home.next} />)
  if (!rows.length) return null
  const shown = all ? rows : rows.slice(0, 2)
  return (
    <div className="inbox rise" data-testid="now">
      {shown}
      {rows.length > 2 && <button className="link inbox__more" onClick={() => setAll(a => !a)}>{all ? 'Свернуть' : `Ещё ${rows.length - 2}`}</button>}
    </div>
  )
}

function UrgentRow({ u, partner }: { u: UrgentAsk; partner: string }) {
  return (
    <div className="inrow inrow--hot" data-testid="urgent-home">
      <span className="inrow__ic"><Icon n="flame" s={17} /></span>
      <button className="inrow__main" onClick={() => go('idea/' + u.idea_id)}><b>{partner} просит ответить: {u.title}</b><span>{[u.when, u.note && `«${u.note}»`].filter(Boolean).join(' · ') || 'Важно для партнёра'}</span></button>
      <span className="inrow__acts">
        <Btn kind="accent" sm onClick={async () => { await answerUrgent(u.idea_id, 'yes') }}>Идём</Btn>
        <button className="iconbtn iconbtn--sm" aria-label="Не смогу" onClick={async () => { await answerUrgent(u.idea_id, 'no') }}><Icon n="x" s={14} /></button>
      </span>
    </div>
  )
}

const stageText: Record<string, [string, string]> = {
  time: ['Отмечаем время', 'Где совпадёте — там и подберу варианты'],
  vote: ['Голосуем', 'Три варианта под ваше общее время'],
  confirm: ['Совпало!', 'Осталось подтвердить обоим'],
}

// the choice in progress, as one row: what step, whose move
function RoundRow({ r, home }: { r: Round; home: HomeT }) {
  const [t, sub] = stageText[r.state] || ['Идёт выбор', '']
  const pn = home.partner?.name || 'Партнёр'
  const mine = r.state === 'time' ? !r.sent : r.state === 'vote' ? !r.voted : r.state === 'confirm' ? !r.confirmed : false
  const step = ({ time: 1, vote: 2, confirm: 3 } as Record<string, number>)[r.state] || 1
  const title = r.chosen ? r.chosen.title : t
  const line = r.state === 'time' && r.partner_sent && !r.sent ? `${pn} ${past(pn, 'предложил')} время — выбери` : r.chosen && r.state === 'time' ? 'Осталось найти время' : sub
  return (
    <button className={'inrow' + (mine ? ' inrow--hot' : '')} onClick={() => go('round/' + r.id)} data-testid="round-card">
      <span className="inrow__ic"><Icon n={r.state === 'time' ? 'clock' : r.state === 'vote' ? 'heart' : 'check'} s={17} /></span>
      <span className="inrow__main"><b>{title}</b><span>Шаг {step} из 3 · {line} · {relLeft(r.expires_at)}</span></span>
      <span className={'pill' + (mine ? ' pill--accent' : '')}>{mine ? 'Твой ход' : 'Ждём ' + ncase(pn, 'acc')}</span>
    </button>
  )
}

// the next date as one slim row: the deck below stays on the first screen
function NextCard({ r }: { r: Round }) {
  if (!r.chosen || !r.starts_at) return null
  const ms = r.starts_at * 1000 - Date.now(), d = Math.floor(ms / 864e5), h = Math.floor((ms % 864e5) / 36e5)
  const left = ms <= 0 ? 'уже сейчас' : d > 0 ? `через ${d} д ${h} ч` : `через ${h} ч ${Math.floor((ms % 36e5) / 6e4)} мин`
  return (
    <button className="nextrow rise" onClick={() => go('round/' + r.id)} data-testid="next-card">
      <span className="nextrow__img"><Cover idea={r.chosen} h={56}><span /><span /></Cover></span>
      <span className="nextrow__main"><small>Ближайшая встреча · {left}</small><b>{r.chosen.title}</b><span>{r.when}</span></span>
      <Icon n="chev" s={16} w={2.4} />
    </button>
  )
}

function Invite() {
  const [busy, setBusy] = useState(false)
  const share = async () => {
    setBusy(true)
    try { const { link } = await post<{ link: string }>('/invite'); shareLink(link, 'Давай планировать выходные вместе — в «Вдвоём»') }
    catch (e) { toast(humanError(e)) } finally { setBusy(false) }
  }
  return (
    <section className="hero rise" data-testid="invite">
      <div className="hero__blob" style={{ background: 'var(--person-a)', top: -60, left: -40 }} />
      <h1 className="display" style={{ maxWidth: 260 }}>Вдвоём интереснее</h1>
      <p className="callout" style={{ opacity: .85, margin: '8px 0 18px', maxWidth: 290 }}>Пригласи партнёра — у вас будет общая копилка идей, а я буду искать время, когда вы оба свободны.</p>
      <Btn kind="primary" busy={busy} onClick={share} icon="share">Пригласить</Btn>
      <p className="caption" style={{ opacity: .7, margin: '12px 0 0' }}>Пока можно складывать идеи — они станут общими.</p>
    </section>
  )
}

function TasteNudge() {
  const [gone, setGone] = useState(() => { try { return localStorage.getItem('vd-taste-nudge') === '1' } catch { return false } })
  if (gone) return null
  const hide = () => { try { localStorage.setItem('vd-taste-nudge', '1') } catch { /* private */ } setGone(true) }
  return (
    <div className="card rise tastecta" style={{ marginTop: 12 }} role="button" onClick={() => go('taste')}>
      <span className="tastecta__e">✨</span>
      <span className="col" style={{ gap: 2, flex: 1, textAlign: 'left' }}><b>Расскажи о своих вкусах — минута</b><span className="caption muted">Театр или стендап, джаз или рок — и подборки сразу попадут в точку</span></span>
      <button className="iconbtn" aria-label="Не сейчас" onClick={e => { e.stopPropagation(); hide() }}><Icon n="x" s={14} /></button>
    </div>
  )
}
