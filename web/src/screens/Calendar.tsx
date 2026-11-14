import { useMemo, useState } from 'react'
import type { CalEvent, Home, Idea, Round, Stats, Wish } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, humanError } from '../lib/api'
import { go } from '../lib/router'
import { money, plural } from '../lib/format'
import { MonthGrid, type DayMark } from '../ui/Charts'
import { Btn, Cover, Empty, Segmented, Skel, toast } from '../ui/kit'
import { EventCard } from '../ui/EventCard'
import { haptic } from '../lib/tg'
import { CalBanner, CalButton } from '../ui/CalLink'

const ym = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
const KIND: Record<string, string> = { planned: 'Встреча', done: 'Были', deadline: 'Дедлайн', open: 'Ждёт подтверждения' }

type Mode = 'ours' | 'afisha'
const ymdOf = (unix: number) => { const d = new Date(unix * 1000); return `${ym(d)}-${String(d.getDate()).padStart(2, '0')}` }

// Two calendars on one grid: «Наше» — only what we chose (meetings, chore deadlines); «Афиша» — what is on
// each day, to flip through dates and take an event into our bank with «Хочу».
export function CalendarScreen({ home }: { home: Home | null }) {
  const [mode, setModeS] = useState<Mode>(() => { try { return localStorage.getItem('vd-cal-mode') === 'afisha' ? 'afisha' : 'ours' } catch { return 'ours' } })
  const setMode = (m: Mode) => { setModeS(m); try { localStorage.setItem('vd-cal-mode', m) } catch { /* private */ } }
  const [month, setMonth] = useState(() => { const d = new Date(); return new Date(d.getFullYear(), d.getMonth(), 1) })
  const { data } = useLoad<{ events: CalEvent[]; today: string }>('/calendar?month=' + ym(month), 30000)
  const stats = useLoad<Stats>('/stats')
  const [sel, setSel] = useState('')
  const today = data?.today || ''
  const marks = useMemo(() => {
    const m: Record<string, DayMark> = {}
    for (const e of data?.events || []) {
      const x = (m[e.date] ||= {})
      if (e.kind === 'planned' || e.kind === 'open') x.planned = true
      if (e.kind === 'done') x.done = true
      if (e.kind === 'deadline') x.deadline = true
    }
    return m
  }, [data])
  const events = (data?.events || []).filter(e => sel ? e.date === sel : e.date >= today)
  const mp = stats.data?.months.find(m => m.month === ym(month))
  const mm = mp ? mp.done + mp.planned : 0, hh = Math.round(stats.data?.hours ?? 0)
  const startOn = async () => {
    // a choice already going on: open it on this day's week instead of a dead end
    try { sessionStorage.setItem('vd-focus-day', sel) } catch { /* private mode */ }
    if (home?.round) { go('round/' + home.round.id); return }
    try { const r = await post<{ round: Round }>('/rounds', { day: sel }); emit('home'); go('round/' + r.round.id) } catch (e) { toast(humanError(e)) }
  }
  const canStart = !!sel && sel >= today && !(marks[sel]?.planned) && !!home?.paired
  return (
    <>
      <header className="hdr"><h1 className="display">Календарь</h1><span className="spacer" /><CalButton /></header>
      <div className="rise" style={{ marginBottom: 12 }}>
        <Segmented<Mode> items={[['ours', 'Наше'], ['afisha', 'Афиша']]} value={mode} onChange={setMode} />
      </div>
      {mode === 'afisha' ? <AfishaMonth month={month} setMonth={setMonth} today={today} /> : <>
      <CalBanner />
      <div className="monthstats rise">
        <div><b>{mm}</b><span>{plural(mm, 'встреча', 'встречи', 'встреч')} в месяце</span></div>
        <div><b>{hh}</b><span>{plural(hh, 'час', 'часа', 'часов')} вместе</span></div>
        {!!stats.data?.spent && <div><b style={{ fontSize: 18 }}>{money(stats.data.spent)}</b><span>потрачено</span></div>}
      </div>
      {!data ? <Skel h={330} r={24} /> : <div className="rise" style={{ ['--d' as any]: 1 }}>
        <MonthGrid month={month} marks={marks} today={today} selected={sel} onSelect={d => setSel(s => s === d ? '' : d)} onMonth={n => { setSel(''); setMonth(m => new Date(m.getFullYear(), m.getMonth() + n, 1)) }} />
      </div>}
      <div className="sec"><h2 className="title"><span className="orange-ind" />{sel ? new Date(sel + 'T12:00').toLocaleDateString('ru', { weekday: 'long', day: 'numeric', month: 'long' }) : 'Запланировано'}</h2>
        {sel && <button className="link" onClick={() => setSel('')}>Все</button>}</div>
      {canStart && <div style={{ marginBottom: 10 }}><Btn kind="accent" block icon="cal" onClick={startOn}>{home?.round ? 'Отметить время на этот день' : 'Выбрать вместе на этот день'}</Btn></div>}
      {events.length === 0 ? <div className="card"><Empty title={sel ? 'В этот день пусто' : 'Впереди ничего нет'} text={sel ? (canStart ? 'Можно начать выбор прямо на этот день.' : undefined) : 'Запланированные встречи и дедлайны дел появятся здесь.'} /></div> : (
        <div className="sched">
          {events.map((e, k) => (
            <button key={k} className="sched__row" style={{ ['--d' as any]: k }} onClick={() => go(e.round_id ? 'round/' + e.round_id : 'idea/' + e.idea_id)}>
              <span className="sched__thumb"><Cover idea={{ category: e.category || 'other', title: e.title }} /></span>
              <span className="sched__main"><span className="sched__tag" data-k={e.kind}>{KIND[e.kind]}</span><b>{e.title}</b>
                <span>{new Date(e.date + 'T12:00').toLocaleDateString('ru', { weekday: 'short', day: 'numeric', month: 'short' })}</span></span>
              {e.time && <span className="sched__time">{e.time}</span>}
            </button>
          ))}
        </div>
      )}
      </>}
    </>
  )
}

// «Афиша» mode: dots for days with events (the number of them), a tap on a day lists them; «Хочу» takes one
// into our bank. Only events with a date — exhibitions running for weeks live in the afisha's «Скоро закончатся».
function AfishaMonth({ month, setMonth, today }: { month: Date; setMonth: (f: (m: Date) => Date) => void; today: string }) {
  const res = useLoad<{ ideas: Idea[] }>('/afisha?sort=date&month=' + ym(month))
  const [sel, setSel] = useState('')
  const day = today || ymdOf(Date.now() / 1000)
  const marks = useMemo(() => {
    const m: Record<string, DayMark> = {}
    for (const i of res.data?.ideas || []) if (i.starts_at) { const x = (m[ymdOf(i.starts_at)] ||= {}); x.events = (x.events || 0) + 1 }
    return m
  }, [res.data])
  const list = (res.data?.ideas || []).filter(i => i.starts_at && (sel ? ymdOf(i.starts_at) === sel : ymdOf(i.starts_at) >= day))
  const mark = async (i: Idea, w: Wish) => {
    haptic.tap()
    try {
      await post('/ideas/' + i.id + '/wish', { wish: w })
      res.set(r => r && { ...r, ideas: r.ideas.map(x => x.id === i.id ? { ...x, my_wish: w } : x) })
      emit('ideas'); emit('home')
      if (w !== 'skip') toast(w === 'very' ? 'Очень хочу ✓ — в нашем банке' : 'Хочу ✓ — в нашем банке')
    } catch (e) { toast(humanError(e)) }
  }
  return (<>
    {!res.data ? <Skel h={330} r={24} /> : <div className="rise">
      <MonthGrid month={month} marks={marks} today={today} selected={sel} legend={[['#f0b400', 'события дня']]}
        onSelect={d => setSel(s => s === d ? '' : d)} onMonth={n => { setSel(''); setMonth(m => new Date(m.getFullYear(), m.getMonth() + n, 1)) }} />
    </div>}
    <div className="sec"><h2 className="title"><span className="orange-ind" />{sel ? new Date(sel + 'T12:00').toLocaleDateString('ru', { weekday: 'long', day: 'numeric', month: 'long' }) : 'Афиша месяца'}</h2>
      {sel ? <button className="link" onClick={() => setSel('')}>Все</button> : <button className="link" onClick={() => go('ideas/scout')}>Вся афиша</button>}</div>
    {!res.data ? null : list.length === 0
      ? <div className="card"><Empty title={sel ? 'В этот день событий нет' : 'В этом месяце событий с датой нет'} text="Скаут приносит события каждый день — или спроси помощника «найди новое на …»." /></div>
      : <div className="col">{list.map((i, k) => <EventCard key={i.id} i={i} k={k} onMark={mark} />)}</div>}
  </>)
}

