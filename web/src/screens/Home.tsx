import { useState } from 'react'
import type { Home as HomeT, Round } from '../lib/types'
import { go } from '../lib/router'
import { post, humanError } from '../lib/api'
import { useOnline } from '../lib/live'
import { relLeft, ncase, past } from '../lib/format'
import { shareLink } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Avatar, Btn, Cover, Skel, toast } from '../ui/kit'
import { UrgentList } from '../ui/Urgent'
import { Deck } from '../ui/Deck'
import { Going } from '../ui/Going'

// The main screen is the deck: swipe ideas and events, a match goes straight to picking the time. Above it only
// what needs you now (an answer, a meeting in progress, the next date); the assistant and «add» live in one bar.
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
        <button className="pairava" style={{ border: 0, background: 'none', padding: 0, cursor: 'pointer' }} onClick={() => go('us')} aria-label="Мы">
          <Avatar p={home.me} /><Avatar p={home.partner} ghost={!home.partner} online={partnerHere} />
        </button>
      </header>
      <div className="askbar rise">
        <button id="ask-bar" className="askbar__q" onClick={() => go('assistant')}><Icon n="spark" s={19} /><span>Спроси или вставь ссылку</span></button>
        <button className="askbar__plus" aria-label="Добавить идею" onClick={onAdd}><Icon n="plus" s={20} w={2.3} /></button>
      </div>
      {!!home.urgent?.length && <div style={{ marginBottom: 12 }}><UrgentList items={home.urgent} partner={home.partner?.name || 'Партнёр'} /></div>}
      {!home.paired ? <Invite /> : home.round ? <RoundCard r={home.round} home={home} /> : home.next ? <NextCard r={home.next} /> : null}
      {home.paired && <Now home={home} />}
      {home.paired && <>
        {/* no section title: the deck is the screen; matches so far — one quiet link */}
        <div className="row" style={{ margin: home.round || home.next || home.urgent?.length ? '14px 2px 8px' : '0 2px 8px', minHeight: 20 }}>
          <span className="caption muted">Свайпни: вправо — хочу, влево — нет</span><span className="spacer" />
          {home.counts.both_want > 0 && <button className="link" onClick={() => go('ideas/both')}>Совпало · {home.counts.both_want}</button>}
        </div>
        <Deck partner={home.partner} />
        <div className="row" style={{ gap: 8, marginTop: 14 }}>
          <Btn kind="surface" block icon="dice" onClick={() => onSurprise('random')}>Удиви нас</Btn>
          {!home.round && <Btn kind="surface" block icon="cal" onClick={onStart}>Выбрать вместе</Btn>}
        </div>
      </>}
      {home.taste_set === false && <TasteNudge />}
      {(home.going?.length || 0) > 1 && <Going list={home.going!} />}
    </>
  )
}

const stageText: Record<string, [string, string]> = {
  time: ['Отмечаем время', 'Где совпадёте — там и подберу варианты'],
  vote: ['Голосуем', 'Три варианта под ваше общее время'],
  confirm: ['Совпало!', 'Осталось подтвердить обоим'],
}

function RoundCard({ r, home }: { r: Round; home: HomeT }) {
  const [t, sub] = stageText[r.state] || ['Идёт выбор', '']
  const mine = r.state === 'time' ? !r.sent : r.state === 'vote' ? !r.voted : r.state === 'confirm' ? !r.confirmed : false
  const theirs = r.state === 'time' ? !r.partner_sent : r.state === 'vote' ? !r.partner_voted : !r.partner_confirmed
  const step = ({ time: 1, vote: 2, confirm: 3 } as Record<string, number>)[r.state] || 1
  return (
    <div className="frame rise" onClick={() => go('round/' + r.id)} style={{ cursor: 'pointer' }} data-testid="round-card">
      <div className="frame__in">
        <div className="row" style={{ marginBottom: 12 }}>
          <span className="pill pill--accent"><span className="dot dot--pulse" />шаг {step} из 3</span>
          <span className="spacer" /><span className="caption muted">{relLeft(r.expires_at)}</span>
        </div>
        <h2 className="title">{r.chosen && r.state === 'time' ? r.chosen.title : t}</h2>
        <p className="callout muted" style={{ margin: '4px 0 14px' }}>{r.chosen && r.state === 'time' ? 'Осталось найти время' : sub}</p>
        <div className="hatch" style={{ marginBottom: 14 }}><i style={{ ['--p' as any]: `${(step - 1 + (mine ? 0 : .5) + (theirs ? 0 : .5)) / 3 * 100}%` }} /></div>
        <div className="row">
          <Who p="Ты" done={!mine} color={home.me.color} />
          <Who p={home.partner?.name || ''} done={!theirs} color={home.partner?.color || 'b'} />
          <span className="spacer" />
          <Btn sm kind={mine ? 'accent' : 'soft'}>{mine ? 'Твой ход' : 'Открыть'}</Btn>
        </div>
      </div>
    </div>
  )
}

// «Что сделать сейчас»: every open loop as one row with one tap — answers, tickets, marks, news from the scout
function Now({ home }: { home: HomeT }) {
  const pn = home.partner?.name || 'Партнёр'
  const rows: { k: string; icon: string; title: string; sub: string; go: string; hot?: boolean }[] = []
  const r = home.round
  if (r?.state === 'time' && r.partner_sent && !r.sent) rows.push({ k: 'time', icon: 'clock', title: `${pn} ${past(pn, 'предложил')} время`, sub: 'Выбери подходящее или предложи своё', go: 'round/' + r.id, hot: true })
  if (r?.state === 'time' && !r.partner_sent && !r.sent) rows.push({ k: 'time0', icon: 'clock', title: 'Отметь, когда тебе удобно', sub: r.chosen ? r.chosen.title : 'Выбираем, куда пойти', go: 'round/' + r.id, hot: true })
  if (r?.state === 'vote' && !r.voted) rows.push({ k: 'vote', icon: 'heart', title: 'Выбери из трёх вариантов', sub: 'Голос партнёра откроется после твоего', go: 'round/' + r.id, hot: true })
  if (r?.state === 'confirm' && !r.confirmed) rows.push({ k: 'conf', icon: 'check', title: 'Подтверди встречу', sub: r.chosen?.title || '', go: 'round/' + r.id, hot: true })
  for (const g of home.going || []) if (g.state === 'planned' && g.book && !g.tickets) rows.push({ k: 'tk' + g.id, icon: 'wallet', title: g.book.label, sub: [g.chosen?.title, g.when].filter(Boolean).join(' · '), go: 'round/' + g.id })
  if (!rows.length) return null
  return (<>
    <div className="sec"><h2 className="title"><span className="orange-ind" />Сейчас</h2></div>
    <div className="list rise" data-testid="now">
      {rows.slice(0, 5).map(x => <button key={x.k} className="li" onClick={() => go(x.go)}>
        <span className="li__ico" style={x.hot ? { background: 'color-mix(in srgb, var(--accent) 16%, transparent)', color: 'var(--accent)' } : undefined}><Icon n={x.icon} s={18} /></span>
        <span className="li__main"><b>{x.title}</b>{x.sub && <span>{x.sub}</span>}</span><Icon n="chev" s={15} w={2.2} /></button>)}
    </div>
  </>)
}

function Who({ p, done, color }: { p: string; done: boolean; color: string }) {
  return (
    <span className="row caption" style={{ gap: 6, marginRight: 8 }}>
      <span className="dot" style={{ ['--c' as any]: `var(--person-${color})`, opacity: done ? 1 : .35 }} />
      {p}: <b style={{ fontWeight: 650 }}>{done ? 'готово' : 'ждём'}</b>
    </span>
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
