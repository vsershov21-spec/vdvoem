import { useMemo, useState } from 'react'
import type { CalEvent, Home, Round, Stats } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, humanError } from '../lib/api'
import { go } from '../lib/router'
import { money } from '../lib/format'
import { MonthGrid, type DayMark } from '../ui/Charts'
import { Btn, Cover, Empty, Skel, toast } from '../ui/kit'

const ym = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
const KIND: Record<string, string> = { planned: 'Встреча', done: 'Были', deadline: 'Дедлайн', ends: 'Заканчивается', open: 'Ждёт подтверждения' }

export function CalendarScreen({ home }: { home: Home | null }) {
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
      if (e.kind === 'ends') x.ends = true
    }
    return m
  }, [data])
  const events = (data?.events || []).filter(e => sel ? e.date === sel : e.date >= today)
  const mp = stats.data?.months.find(m => m.month === ym(month))
  const startOn = async () => {
    try { const r = await post<{ round: Round }>('/rounds', { day: sel }); emit('home'); go('round/' + r.round.id) } catch (e) { toast(humanError(e)) }
  }
  const canStart = !!sel && sel >= today && !(marks[sel]?.planned) && !!home?.paired
  return (
    <>
      <header className="hdr"><h1 className="display">Календарь</h1></header>
      <div className="monthstats rise">
        <div><b>{mp ? mp.done + mp.planned : 0}</b><span>встреч в месяце</span></div>
        <div><b>{stats.data?.hours ?? 0}</b><span>часов вместе</span></div>
        <div><b style={{ fontSize: 18 }}>{stats.data?.spent ? money(stats.data.spent) : '—'}</b><span>потрачено</span></div>
      </div>
      {!data ? <Skel h={330} r={24} /> : <div className="rise" style={{ ['--d' as any]: 1 }}>
        <MonthGrid month={month} marks={marks} today={today} selected={sel} onSelect={d => setSel(s => s === d ? '' : d)} onMonth={n => { setSel(''); setMonth(m => new Date(m.getFullYear(), m.getMonth() + n, 1)) }} />
      </div>}
      <div className="sec"><h2 className="title"><span className="orange-ind" />{sel ? new Date(sel + 'T12:00').toLocaleDateString('ru', { weekday: 'long', day: 'numeric', month: 'long' }) : 'Запланировано'}</h2>
        {sel && <button className="link" onClick={() => setSel('')}>Все</button>}</div>
      {canStart && <div style={{ marginBottom: 10 }}><Btn kind="accent" block icon="cal" onClick={startOn}>Выбрать вместе на этот день</Btn></div>}
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
    </>
  )
}
