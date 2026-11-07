import { useState } from 'react'
import type { Home as HomeT, Idea, Round, Stats, Wish } from '../lib/types'
import { go } from '../lib/router'
import { post, humanError } from '../lib/api'
import { emit, useLoad, useOn } from '../lib/data'
import { relLeft, plural, money, ncase } from '../lib/format'
import { shareLink } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Avatar, Btn, Cover, CountUp, Empty, Skel, WishPicker, toast } from '../ui/kit'
import { AreaChart, Bars, Gauge, Metric, Pulse } from '../ui/Charts'

export function Home({ home, onAdd, onSurprise, onStart }: { home: HomeT | null; onAdd: () => void; onSurprise: (m: 'random' | 'best') => void; onStart: () => void }) {
  const stats = useLoad<Stats>('/stats', 60000)
  useOn('home', stats.refresh)
  if (!home) return (<><Skel h={44} /><Skel h={190} r={36} /><Skel h={84} /><Skel h={200} /></>)
  const hour = new Date().getHours()
  const hello = hour < 5 ? 'Доброй ночи' : hour < 12 ? 'Доброе утро' : hour < 18 ? 'Добрый день' : 'Добрый вечер'
  return (
    <>
      <header className="hdr" style={{ height: 'auto', margin: '4px 0 14px' }}>
        <div style={{ flex: 1 }}>
          <h1 className="title">{hello}, {home.me.name} 👋</h1>
          <p className="caption muted" style={{ margin: '2px 0 0' }}>{home.partner ? `Планируем вдвоём с ${ncase(home.partner.name, 'ins')}` : 'Пригласи партнёра — и начнём'}</p>
        </div>
        <button className="pairava" style={{ border: 0, background: 'none', padding: 0, cursor: 'pointer' }} onClick={() => go('us')} aria-label="Мы">
          <Avatar p={home.me} /><Avatar p={home.partner} ghost={!home.partner} />
        </button>
      </header>
      {!home.paired ? <Invite /> : home.round ? <RoundCard r={home.round} home={home} /> : home.next ? <NextCard r={home.next} /> : <Hero home={home} onStart={onStart} />}
      {home.paired && (
        <div className="row rise" style={{ ['--d' as any]: 1, marginTop: 12, gap: 8 }}>
          <Btn kind="surface" block icon="dice" onClick={() => onSurprise('random')}>Удиви нас</Btn>
          <Btn kind="surface" block icon="spark" onClick={() => onSurprise('best')}>Подскажи</Btn>
        </div>
      )}
      <Dashboard s={stats.data} home={home} />
      <ToMark ideas={home.to_mark || []} onAdd={onAdd} />
    </>
  )
}

function Dashboard({ s, home }: { s: Stats | null; home: HomeT }) {
  if (!s) return <div className="metrics" style={{ marginTop: 16 }}>{[0, 1, 2, 3].map(k => <Skel key={k} h={104} mb={0} />)}</div>
  const months = s.months.map(m => m.done + m.planned)
  return (<>
    <div className="sec"><h2 className="title"><span className="orange-ind" />Наша статистика</h2></div>
    <div className="metrics">
      <Metric label="Встречи" icon="cal" tone="orange" value={<CountUp to={s.meetings} />} unit={`${s.this_month} в этом мес.`} graph={<Bars values={months} tone="orange" />} onClick={() => go('calendar')} />
      <Metric label="Совпадение" icon="heart" tone="pink" value={<><CountUp to={s.match} />%</>} unit={`${s.both_want} хотите оба`} graph={<Gauge value={s.match} tone="pink" />} onClick={() => go('ideas/both')} />
      <Metric label="Идеи" icon="folder" tone="green" value={<CountUp to={s.ideas} />} unit={s.unmarked ? `${s.unmarked} без отметки` : 'все отмечены'} graph={s.week.some(Boolean) ? <Bars values={s.week} tone="green" /> : <Pulse tone="green" />} onClick={() => go('ideas')} />
      <Metric label="Вместе" icon="clock" tone="blue" value={<CountUp to={Math.round(s.hours)} />} unit={s.spent ? `ч · ${money(s.spent)}` : 'часов'} graph={<Bars values={s.months.map(m => m.done)} tone="blue" />} onClick={() => go('us')} />
    </div>
    <div className="chartcard rise" style={{ ['--d' as any]: 3, marginTop: 10 }}>
      <div className="chartcard__head"><h3>Наши встречи</h3><span className="chip-filter">6 месяцев</span></div>
      {months.some(Boolean) ? <AreaChart labels={s.months.map(m => m.label)} values={months} peakLabel={v => `${v} ${plural(v, 'встреча', 'встречи', 'встреч')}`} />
        : <Empty title="Пока без встреч" text={home.paired ? 'Первая встреча появится на графике, как только вы её запланируете.' : 'Пригласи партнёра, чтобы начать.'} />}
    </div>
  </>)
}

function Hero({ home, onStart }: { home: HomeT; onStart: () => void }) {
  return (
    <section className="hero rise">
      <div className="hero__blob" style={{ background: 'var(--person-a)', top: -60, left: -40 }} />
      <div className="hero__blob" style={{ background: 'var(--person-b)', bottom: -70, right: -30, animationDelay: '-4s' }} />
      <p className="micro" style={{ opacity: .8, margin: '0 0 8px' }}>{home.me.name} и {home.partner?.name}</p>
      <h1 className="display" style={{ maxWidth: 260 }}>Куда сходим на выходных?</h1>
      <p className="callout" style={{ opacity: .85, margin: '8px 0 18px', maxWidth: 280 }}>Отметьте время — я найду, когда вы оба свободны, и предложу три варианта.</p>
      <Btn kind="primary" shine onClick={onStart} icon="cal">Выбрать вместе</Btn>
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

function Who({ p, done, color }: { p: string; done: boolean; color: string }) {
  return (
    <span className="row caption" style={{ gap: 6, marginRight: 8 }}>
      <span className="dot" style={{ ['--c' as any]: `var(--person-${color})`, opacity: done ? 1 : .35 }} />
      {p}: <b style={{ fontWeight: 650 }}>{done ? 'готово' : 'ждём'}</b>
    </span>
  )
}

function NextCard({ r }: { r: Round }) {
  if (!r.chosen || !r.starts_at) return null
  const ms = r.starts_at * 1000 - Date.now(), d = Math.floor(ms / 864e5), h = Math.floor((ms % 864e5) / 36e5)
  return (
    <div className="ticket rise card--tap" onClick={() => go('round/' + r.id)} data-testid="next-card">
      <Cover idea={r.chosen} h={176}>
        <span className="pill pill--glass" style={{ alignSelf: 'flex-start' }}><Icon n="cal" s={13} w={2.2} />Запланировано</span>
        <div><div className="cover__title" style={{ fontSize: 22, lineHeight: '26px' }}>{r.chosen.title}</div>
          <div className="caption" style={{ opacity: .9, marginTop: 4 }}>{r.when}</div></div>
      </Cover>
      <div className="row" style={{ padding: '12px 16px', gap: 16 }}>
        <span className="caption muted">До встречи</span><span className="spacer" />
        <b className="headline">{ms <= 0 ? 'уже сейчас' : d > 0 ? `${d} д ${h} ч` : `${h} ч ${Math.floor((ms % 36e5) / 6e4)} мин`}</b>
      </div>
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
    <section className="hero rise" data-testid="invite">
      <div className="hero__blob" style={{ background: 'var(--person-a)', top: -60, left: -40 }} />
      <h1 className="display" style={{ maxWidth: 260 }}>Вдвоём интереснее</h1>
      <p className="callout" style={{ opacity: .85, margin: '8px 0 18px', maxWidth: 290 }}>Пригласи партнёра — у вас будет общая копилка идей, а я буду искать время, когда вы оба свободны.</p>
      <Btn kind="primary" busy={busy} onClick={share} icon="share">Пригласить</Btn>
      <p className="caption" style={{ opacity: .7, margin: '12px 0 0' }}>Пока можно складывать идеи — они станут общими.</p>
    </section>
  )
}

function ToMark({ ideas, onAdd }: { ideas: Idea[]; onAdd: () => void }) {
  const [gone, setGone] = useState<Record<number, boolean>>({})
  const mark = async (i: Idea, w: Wish) => {
    if (!w) return
    setTimeout(() => setGone(g => ({ ...g, [i.id]: true })), w === 'very' ? 650 : 150)
    try { await post(`/ideas/${i.id}/wish`, { wish: w }); setTimeout(() => emit('home'), 1100) } catch (e) { toast(humanError(e)) }
  }
  return (<>
    <div className="sec"><h2 className="title"><span className="orange-ind" />Отметь, чего хочется</h2>{ideas.length > 0 && <button className="link" onClick={() => go('ideas/unmarked')}>Все</button>}</div>
    {ideas.length === 0 ? (
      <div className="card"><Empty title="Всё отмечено" text="Кидай в бота ссылки на места и события — или добавь идею здесь."><Btn kind="primary" sm icon="plus" onClick={onAdd}>Добавить идею</Btn></Empty></div>
    ) : (
      <div className="rail">
        {ideas.map((i, k) => (
          <div key={i.id} className="icard" data-testid="mark-card" style={{ width: 268, ['--d' as any]: k, transition: 'opacity .4s, transform .45s var(--ease-out)', opacity: gone[i.id] ? 0 : 1, transform: gone[i.id] ? 'translateY(-12px) scale(.95)' : undefined }}
            onClick={() => go('idea/' + i.id)}>
            <Cover idea={i} h={140}><span /><div className="cover__title">{i.title}</div></Cover>
            <div style={{ padding: '0 4px' }}><p className="caption muted" style={{ margin: '0 0 10px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{i.meta}</p>
              <WishPicker small value={i.my_wish} onChange={w => mark(i, w)} /></div>
          </div>
        ))}
      </div>
    )}
  </>)
}
