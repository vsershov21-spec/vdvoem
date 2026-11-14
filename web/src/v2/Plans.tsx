import { useState } from 'react'
import { useLoad, useOn, emit } from '../lib/data'
import { post, humanError } from '../lib/api'
import { go, back } from '../lib/router'
import { openLink } from '../lib/tg'
import type { CalEvent, Idea } from '../lib/types'
import type { HomeV2 } from './types'
import { Sheet, Pic, Empty, say } from './ui'
import { goMeet } from './Ideas'
import type { FreeRes } from './Meet'

const PARTS = ['Утро', 'День', 'Вечер']
const fem = (n: string) => /[ая]$/i.test(n) ? 'а' : ''
const pad = (n: number) => String(n).padStart(2, '0')

// Планы: meetings and busy days only. Chores live in their own tab and are not shown here.
export function Plans({ home }: { home: HomeV2 | null }) {
  const now = new Date(), ym = `${now.getFullYear()}-${pad(now.getMonth() + 1)}`
  const cal = useLoad<{ events: CalEvent[]; today: string }>('/calendar?month=' + ym, 30000)
  const free = useLoad<FreeRes>('/free', 30000)
  useOn('home', () => { cal.refresh(); free.refresh() })
  const [day, setDay] = useState('')
  const [sheet, setSheet] = useState(false)
  const feed = useLoad<{ url: string; webcal: string; subscribed: boolean }>(sheet ? '/calendar/feed' : null)
  const pn = home?.partner?.name || 'партнёр', me = home?.me.name || ''
  const today = cal.data?.today || `${ym}-${pad(now.getDate())}`
  const meets = (cal.data?.events || []).filter(e => (e.kind === 'planned' || e.kind === 'done' || e.kind === 'open') && e.round_id)
  const first = new Date(now.getFullYear(), now.getMonth(), 1), n = new Date(now.getFullYear(), now.getMonth() + 1, 0).getDate()
  const open = home?.round && home.round.state !== 'planned' ? home.round : null
  const busy = free.data?.busy || [], pbusy = free.data?.partner_busy || []
  const ahead = meets.filter(e => e.kind === 'planned' && e.date >= today)
  const toggle = async () => { try { free.set(await post<FreeRes>('/busy-day', { day, on: !busy.includes(day) })) } catch (e) { say(humanError(e)) } }
  return (
    <div className="scr" data-testid="v2-plans">
      <div className="v-row"><h2>Планы</h2><span className="sp" /><button className="b s" aria-label="Календарь телефона" onClick={() => setSheet(true)}>Календарь</button></div>
      <div className="v-card"><b>{now.toLocaleDateString('ru', { month: 'long' })}</b><div className="cal">
        {['пн', 'вт', 'ср', 'чт', 'пт', 'сб', 'вс'].map(d => <span className="wd" key={d}>{d}</span>)}
        {Array.from({ length: (first.getDay() + 6) % 7 }, (_, k) => <span key={'e' + k} />)}
        {Array.from({ length: n }, (_, k) => { const d = `${ym}-${pad(k + 1)}`, ev = meets.find(e => e.date === d)
          return <button key={d} data-day={d} className={(ev ? (ev.kind === 'open' ? 'evw ' : 'ev ') : '') + (busy.includes(d) || pbusy.includes(d) ? 'bz ' : '') + (d === today ? 'today ' : '') + (day === d ? 'sel' : '')} onClick={() => setDay(day === d ? '' : d)}>{k + 1}</button> })}
      </div><p className="m">Заливка: встреча. Серый: кто-то занят.</p></div>
      {day && <div className="v-card" data-testid="day-panel">
        <div className="v-row"><b>{new Date(day + 'T12:00').toLocaleDateString('ru', { day: 'numeric', month: 'long' })}</b><span className="sp" /><button className="back" onClick={() => setDay('')}>Закрыть</button></div>
        {meets.filter(e => e.date === day).map(e => <button className="in" key={e.round_id} style={{ boxShadow: 'none', padding: '4px 0', background: 'none' }} onClick={() => go('meet/' + e.round_id)}><Pic idea={{ title: e.title, category: e.category }} className="th" /><span><b>{e.title}</b><small>{e.time || ''}</small></span>›</button>)}
        {!meets.some(e => e.date === day) && !pbusy.includes(day) && <p className="m">Встреч нет</p>}
        {pbusy.includes(day) && <p className="m">{pn} занят{fem(pn)}</p>}
        {day >= today && <div className="srow"><span className="tx"><b>Я занят{fem(me)} в этот день</b><small>{pn} увидит только «занят{fem(me)}»</small></span><button className={'tg' + (busy.includes(day) ? ' on' : '')} role="switch" aria-checked={busy.includes(day)} aria-label="Я занят в этот день" data-testid="busy-toggle" onClick={toggle} /></div>}
        <button className="b w" onClick={() => go('ideas')}>Что можно в этот день</button>
      </div>}
      <div className="lbl">Встречи</div>
      {open && <button className="in" data-testid="plan-open" onClick={() => go('meet/' + open.id)}>{open.chosen && <Pic idea={open.chosen} className="th" />}<span><b>{open.chosen?.title || 'Встреча'}</b><small>без времени: предложи или ответь</small></span>›</button>}
      {ahead.map(e => <button className="in" key={e.round_id} data-testid="plan-row" onClick={() => go('meet/' + e.round_id)}><Pic idea={{ title: e.title, category: e.category }} className="th" /><span><b>{e.title}</b><small>{new Date(e.date + 'T12:00').toLocaleDateString('ru', { weekday: 'short', day: 'numeric', month: 'short' })}{e.time ? ', ' + e.time : ''}</small></span>›</button>)}
      {!open && !ahead.length && <Empty title="Встреч пока нет"><button className="b p w" onClick={() => go('ideas')}>Выбрать из совпавшего</button></Empty>}
      <button className="b w" data-testid="to-free" onClick={() => go('free')}>Найти общее время</button>
      <Sheet open={sheet} onClose={() => setSheet(false)} testid="cal-sheet">
        <h3>Календарь телефона</h3>
        <div className="v-card"><div className="v-row"><b>Встречи → в телефон</b><span className="sp" /><span className={'v-chip' + (feed.data?.subscribed ? ' on' : '')}>{feed.data?.subscribed ? 'работает' : 'не подключено'}</span></div>
          <p className="m">Договорились: появилась. Перенесли: поменялась.</p>
          {feed.data && !feed.data.subscribed && <button className="b p w" onClick={() => openLink(feed.data!.webcal)}>Добавить в календарь</button>}</div>
        <div className="v-card"><b>Занятость</b><p className="m">Занятые дни отмечаются прямо здесь: тап по дню, «Я занят». Брать их из календаря телефона можно в настройках.</p></div>
        <button className="b ghost w" onClick={() => setSheet(false)}>Закрыть</button>
      </Sheet>
    </div>)
}

// «Общее время»: both mark when they are free; a common window offers what both want
export function Free({ home, from }: { home: HomeV2 | null; from: string }) {
  const { data, set } = useLoad<FreeRes>('/free', 20000)
  const ideas = useLoad<{ ideas: Idea[] }>('/ideas')
  const pn = home?.partner?.name || 'партнёр'
  const w = (x?: string) => x === 'want' || x === 'very'
  const both = (ideas.data?.ideas || []).filter(i => i.kind !== 'chore' && w(i.my_wish) && w(i.partner_wish))
  const tap = async (day: string, part: number, on: boolean) => { try { set(await post<FreeRes>('/free', { day, part, on })) } catch (e) { say(humanError(e)) } }
  const hit = data?.common[0], [hd, hp] = hit ? hit.split(':') : ['', '0']
  const label = hit ? `${data!.days.find(d => d.day === hd)?.label}, ${PARTS[+hp].toLowerCase()}` : ''
  const start = async (i: Idea) => {
    await goMeet(i)
    const id = Number(location.pathname.split('/').pop())
    if (id) { try { await post(`/meet/${id}/time`, { day: hd, part: +hp }); say(`Время предложено: ${pn} ответит`); emit('home') } catch (e) { say(humanError(e)) } }
  }
  return (
    <div className="scr" data-testid="v2-free">
      <div className="subh"><button className="back" aria-label="Назад" onClick={() => back('plans')}>‹ {from}</button><b>Общее время</b><span /></div>
      <p className="m">Отметь, когда свободен. Синяя рамка: свободн{fem(pn) || 'ен'} {pn}. Штриховка: ты занят.</p>
      {data && <div className="v-card"><div className="grid4"><span />{PARTS.map(p => <b key={p}>{p}</b>)}
        {data.days.map(d => [<span key={d.day}>{d.label}</span>, ...[0, 1, 2].map(p => { const k = d.day + ':' + p, on = data.mine.includes(k), bz = data.busy.includes(d.day)
          return <button key={k} className={'slot' + (on ? ' on' : bz ? ' busy' : '') + (data.partner.includes(k) ? ' their' : '')} aria-pressed={on} aria-label={d.label + ' ' + PARTS[p]} data-slot={k} onClick={() => tap(d.day, p, !on)}>{on ? '✓' : ''}</button> })])}
      </div></div>}
      {data && (hit ? <div className="v-card prop" data-testid="common"><b style={{ color: 'var(--accent)', fontSize: 12.5 }}>Оба свободны: {label}</b>
        {both.length ? both.map(i => <div className="v-row" key={i.id}><Pic idea={i} className="th" /><span style={{ flex: 1, minWidth: 0 }}><b>{i.title}</b><p className="m">хотите оба</p></span><button className="b p s" onClick={() => start(i)}>Идём</button></div>)
          : <button className="b p w" onClick={() => go('')}>Пока не совпало. Отмечать идеи</button>}</div>
        : <div className="v-card"><p className="m">{data.partner.length ? `${pn} отметил${fem(pn)} окон: ${data.partner.length}. Общего пока нет: отметь свои.` : `Отметь свои окна. ${pn} отметит свои, и общее появится здесь.`}</p></div>)}
    </div>)
}
