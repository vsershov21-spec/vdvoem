import { useState } from 'react'
import type { Home as HomeT, Idea, Round, Wish } from '../lib/types'
import { go } from '../lib/router'
import { post, get, humanError } from '../lib/api'
import { emit } from '../lib/data'
import { relLeft, plural } from '../lib/format'
import { shareLink } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Avatar, Btn, Cover, CountUp, Empty, Skel, WishPicker, toast, Sheet } from '../ui/kit'
import wordmark from '../ui/wordmark.svg'
import wordmarkDark from '../ui/wordmark-dark.svg'

export function Home({ home, onAdd }: { home: HomeT | null; onAdd: () => void }) {
  const dark = document.documentElement.dataset.theme === 'dark'
  if (!home) return (<><div className="hdr"><img className="hdr__logo" src={dark ? wordmarkDark : wordmark} alt="вдвоём" /></div><Skel h={190} r={36} /><Skel h={84} /><Skel h={200} /></>)
  return (
    <>
      <header className="hdr">
        <img className="hdr__logo" src={dark ? wordmarkDark : wordmark} alt="вдвоём" />
        <span className="spacer" />
        <button className="pairava" style={{ border: 0, background: 'none', padding: 0, cursor: 'pointer' }} onClick={() => go('us')} aria-label="Мы">
          <Avatar p={home.me} /><Avatar p={home.partner} ghost={!home.partner} />
        </button>
      </header>
      {!home.paired ? <Invite /> : home.round ? <RoundCard r={home.round} home={home} /> : home.next ? <NextCard r={home.next} /> : <Hero home={home} />}
      {home.paired && <Surprise />}
      <div className="stats rise" style={{ ['--d' as any]: 2, marginTop: 12 }}>
        <div className="stat"><b><CountUp to={home.counts.ideas} /></b><span className="caption muted">{plural(home.counts.ideas, 'идея', 'идеи', 'идей')} в копилке</span></div>
        <div className="stat"><b style={{ color: 'var(--accent)' }}><CountUp to={home.counts.both_want} /></b><span className="caption muted">хотите оба</span></div>
        <div className="stat"><b><CountUp to={home.counts.chores} /></b><span className="caption muted">{plural(home.counts.chores, 'дело', 'дела', 'дел')}</span></div>
      </div>
      <ToMark ideas={home.to_mark} onAdd={onAdd} />
      <button className="card card--tap rise row" style={{ ['--d' as any]: 5, marginTop: 12, width: '100%', border: 0, textAlign: 'left', gap: 14 }} onClick={() => go('assistant')}>
        <span className="orb" style={{ width: 44, height: 44, margin: 0 }} />
        <span style={{ flex: 1 }}><b className="headline" style={{ display: 'block' }}>Спроси помощника</b><span className="caption muted">«Куда в субботу вечером до 4000?»</span></span>
        <Icon n="chev" />
      </button>
    </>
  )
}

function Hero({ home }: { home: HomeT }) {
  const start = async () => { try { const r = await post<{ round: Round }>('/rounds'); emit('home'); go('round/' + r.round.id) } catch (e) { toast(humanError(e)) } }
  return (
    <section className="hero rise">
      <div className="hero__blob" style={{ background: 'var(--person-a)', top: -60, left: -40 }} />
      <div className="hero__blob" style={{ background: 'var(--person-b)', bottom: -70, right: -30, animationDelay: '-4s' }} />
      <p className="micro" style={{ opacity: .8, margin: '0 0 8px' }}>{home.me.name} и {home.partner?.name}</p>
      <h1 className="display" style={{ maxWidth: 260 }}>Куда сходим на выходных?</h1>
      <p className="callout" style={{ opacity: .85, margin: '8px 0 18px', maxWidth: 280 }}>Отметьте время — я найду, когда вы оба свободны, и предложу три варианта.</p>
      <Btn kind="primary" shine onClick={start} icon="cal">Выбрать вместе</Btn>
    </section>
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
  const step = { time: 1, vote: 2, confirm: 3 }[r.state as 'time'] || 1
  return (
    <div className="frame rise" onClick={() => go('round/' + r.id)} style={{ cursor: 'pointer' }}>
      <div className="frame__in">
        <div className="row" style={{ marginBottom: 12 }}>
          <span className="pill pill--accent"><span className="dot dot--pulse" />шаг {step} из 3</span>
          <span className="spacer" /><span className="caption muted">{relLeft(r.expires_at)}</span>
        </div>
        <h2 className="title">{r.chosen && r.state === 'time' ? r.chosen.title : t}</h2>
        <p className="callout muted" style={{ margin: '4px 0 14px' }}>{r.chosen && r.state === 'time' ? 'Осталось найти время' : sub}</p>
        <div className="hatch" style={{ marginBottom: 14 }}><i style={{ ['--p' as any]: `${(step - 1 + (mine ? 0 : .5) + (theirs ? 0 : .5)) / 3 * 100}%` }} /></div>
        <div className="row">
          <Who p={home.me.name} done={!mine} color={home.me.color} me />
          <Who p={home.partner?.name || ''} done={!theirs} color={home.partner?.color || 'b'} />
          <span className="spacer" />
          <Btn sm kind={mine ? 'accent' : 'soft'}>{mine ? 'Твой ход' : 'Открыть'}</Btn>
        </div>
      </div>
    </div>
  )
}

function Who({ p, done, color, me }: { p: string; done: boolean; color: string; me?: boolean }) {
  return (
    <span className="row caption" style={{ gap: 6, marginRight: 8 }}>
      <span className="dot" style={{ ['--c' as any]: `var(--person-${color})`, opacity: done ? 1 : .35 }} />
      {me ? 'Ты' : p}: <b style={{ fontWeight: 650 }}>{done ? 'готово' : 'ждём'}</b>
    </span>
  )
}

function NextCard({ r }: { r: Round }) {
  if (!r.chosen) return null
  return (
    <div className="ticket rise card--tap" onClick={() => go('round/' + r.id)}>
      <Cover idea={r.chosen} h={170}>
        <span className="pill pill--glass" style={{ alignSelf: 'flex-start' }}><Icon n="cal" s={13} w={2.2} />Запланировано</span>
        <div><div className="cover__title" style={{ fontSize: 22, lineHeight: '26px' }}>{r.chosen.title}</div>
          <div className="caption" style={{ opacity: .9, marginTop: 4 }}>{r.when}</div></div>
      </Cover>
    </div>
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
    <section className="hero rise">
      <div className="hero__blob" style={{ background: 'var(--person-a)', top: -60, left: -40 }} />
      <h1 className="display" style={{ maxWidth: 260 }}>Вдвоём интереснее</h1>
      <p className="callout" style={{ opacity: .85, margin: '8px 0 18px', maxWidth: 290 }}>Пригласи партнёра — у вас будет общая копилка идей, а я буду искать время, когда вы оба свободны.</p>
      <Btn kind="primary" busy={busy} onClick={share} icon="share">Пригласить</Btn>
      <p className="caption" style={{ opacity: .7, margin: '12px 0 0' }}>Пока можно складывать идеи — они станут общими.</p>
    </section>
  )
}

// «Удиви нас»: one idea, best or random; «Давай!» opens a round for it
function Surprise() {
  const [s, setS] = useState<{ idea: Idea; why: string; mode: string } | null>(null)
  const [chain, setChain] = useState<number[]>([])
  const [open, setOpen] = useState(false)
  const pull = async (mode: string, ex: number[]) => {
    try { const r = await get<{ idea: Idea; why: string; mode: string }>(`/suggest?mode=${mode}&exclude=${ex.join(',')}`); setS(r); setChain([...ex, r.idea.id]); setOpen(true) }
    catch (e) { toast(ex.length ? 'Больше подходящих идей нет.' : humanError(e)) }
  }
  const goWith = async () => {
    try { const r = await post<{ round: Round }>('/rounds/with/' + s!.idea.id); setOpen(false); emit('home'); go('round/' + r.round.id) }
    catch (e) { toast(humanError(e)) }
  }
  return (<>
    <div className="row rise" style={{ ['--d' as any]: 1, marginTop: 12, gap: 8 }}>
      <Btn kind="surface" block icon="dice" onClick={() => pull('random', [])}>Удиви нас</Btn>
      <Btn kind="surface" block icon="spark" onClick={() => pull('best', [])}>Что посоветуешь?</Btn>
    </div>
    <Sheet open={open} onClose={() => setOpen(false)}>
      {s && <div className="col" key={s.idea.id}>
        <p className="micro muted" style={{ margin: 0 }}>{s.mode === 'random' ? '🎲 Случайно из копилки' : 'Мой совет'}</p>
        <div className="rise"><Cover idea={s.idea} h={200} big><span /><div className="cover__title" style={{ fontSize: 22, lineHeight: '26px' }}>{s.idea.title}</div></Cover></div>
        <p className="caption muted" style={{ margin: 0 }}>{s.idea.meta}</p>
        <p className="callout" style={{ margin: 0 }}>{s.why}</p>
        <Btn kind="accent" block shine onClick={goWith} icon="check">Давай!</Btn>
        <div className="row"><Btn kind="soft" block onClick={() => pull('best', chain)}>Другую</Btn><Btn kind="soft" block icon="dice" onClick={() => pull('random', chain)}>Рандом</Btn></div>
      </div>}
    </Sheet>
  </>)
}

function ToMark({ ideas, onAdd }: { ideas: Idea[]; onAdd: () => void }) {
  const [list, setList] = useState(ideas)
  const [gone, setGone] = useState<Record<number, boolean>>({})
  if (list !== ideas && ideas.length !== list.length) setList(ideas)
  const mark = async (i: Idea, w: Wish) => {
    if (!w) return
    setGone(g => ({ ...g, [i.id]: true }))
    try { await post(`/ideas/${i.id}/wish`, { wish: w }); setTimeout(() => emit('home'), 450) } catch (e) { toast(humanError(e)) }
  }
  return (<>
    <div className="sec"><h2 className="title">Отметь, чего хочется</h2>{ideas.length > 0 && <button className="link" onClick={() => go('ideas')}>Все</button>}</div>
    {ideas.length === 0 ? (
      <div className="card"><Empty title="Всё отмечено" text="Кидай в бота ссылки на места и события — или добавь идею здесь."><Btn kind="primary" sm icon="plus" onClick={onAdd}>Добавить идею</Btn></Empty></div>
    ) : (
      <div className="rail">
        {list.map((i, k) => (
          <div key={i.id} className="icard" style={{ width: 268, ['--d' as any]: k, transition: 'opacity .4s, transform .45s var(--ease-out)', opacity: gone[i.id] ? 0 : 1, transform: gone[i.id] ? 'translateY(-12px) scale(.95)' : undefined }}
            onClick={() => go('idea/' + i.id)}>
            <Cover idea={i} h={140}><span />{<div className="cover__title">{i.title}</div>}</Cover>
            <div style={{ padding: '0 4px' }}><p className="caption muted" style={{ margin: '0 0 10px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{i.meta}</p>
              <WishPicker small value={i.my_wish} onChange={w => mark(i, w)} /></div>
          </div>
        ))}
      </div>
    )}
  </>)
}
