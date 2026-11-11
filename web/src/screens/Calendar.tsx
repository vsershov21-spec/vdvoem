import { useMemo, useState } from 'react'
import type { CalEvent, Home, Idea, Round, Stats, Wish } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, del, humanError } from '../lib/api'
import { go } from '../lib/router'
import { money, plural } from '../lib/format'
import { MonthGrid, type DayMark } from '../ui/Charts'
import { Btn, Cover, Empty, Segmented, Skel, Sheet, toast } from '../ui/kit'
import { EventCard } from '../ui/EventCard'
import { openLink, haptic } from '../lib/tg'

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
      <header className="hdr"><h1 className="display">Календарь</h1></header>
      <div className="rise" style={{ marginBottom: 12 }}>
        <Segmented<Mode> items={[['ours', 'Наше'], ['afisha', 'Афиша']]} value={mode} onChange={setMode} />
      </div>
      {mode === 'afisha' ? <AfishaMonth month={month} setMonth={setMonth} today={today} /> : <>
      <PhoneCalendar />
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

// A permanent link to the phone's calendar: meetings appear, move and disappear there by themselves.
function PhoneCalendar() {
  const feed = useLoad<{ url: string; webcal: string; subscribed: boolean; busy?: boolean; partner_busy?: boolean }>('/calendar/feed')
  const [busyOpen, setBusyOpen] = useState(false)
  const [open, setOpen] = useState(false)
  const f = feed.data
  const ios = /iPhone|iPad|Mac/.test(navigator.userAgent)
  const copy = async (t: string) => { try { await navigator.clipboard.writeText(t); haptic.ok(); toast('Ссылка скопирована') } catch { toast(t) } }
  return (<>
    {/* one row for both directions: meetings go to the phone, busy hours come from it */}
    <button className="calsub rise" onClick={() => setOpen(true)} data-testid="calsub">
      <span className="calsub__ic">📅</span>
      <span className="calsub__t"><b>{f?.subscribed && f?.busy ? 'Календарь телефона подключён' : f?.subscribed ? 'Встречи уже в телефоне' : 'Подключить календарь телефона'}</b>
        <span>{f?.subscribed && f?.busy ? 'Встречи туда, занятость оттуда — всё само' : f?.subscribed ? 'Можно ещё показывать твою занятость' : 'Встречи появятся там сами, а занятое отметится при выборе'}{f?.partner_busy ? ' · у партнёра подключено' : ''}</span></span>
      <span className="calsub__go">›</span>
    </button>
    <BusySheet open={busyOpen} onClose={() => setBusyOpen(false)} connected={!!f?.busy} onDone={feed.refresh} />
    <Sheet open={open} onClose={() => setOpen(false)}>
      <h2 className="title" style={{ margin: '0 0 6px' }}>Календарь на телефоне</h2>
      <p className="callout muted" style={{ margin: '0 0 14px' }}>Подписка на ваши встречи: когда вы договоритесь, встреча появится в календаре, перенесёте или отмените в приложении — изменится и там (обновляется каждые 15 минут). Ссылка личная.</p>
      {!f ? <Skel h={48} /> : <div className="col" style={{ gap: 10 }}>
        {ios ? <Btn kind="accent" block icon="cal" onClick={() => openLink(f.webcal)}>Добавить в Календарь iPhone</Btn>
          : <Btn kind="accent" block icon="cal" onClick={() => openLink('https://calendar.google.com/calendar/r?cid=' + encodeURIComponent(f.webcal))}>Добавить в Google Календарь</Btn>}
        <Btn kind="soft" block onClick={() => openLink(ios ? 'https://calendar.google.com/calendar/r?cid=' + encodeURIComponent(f.webcal) : f.webcal)}>{ios ? 'Google Календарь' : 'Другой календарь (webcal)'}</Btn>
        <Btn kind="surface" block icon="link" onClick={() => copy(f.url)}>Скопировать ссылку</Btn>
        <p className="caption muted" style={{ margin: '4px 0 0' }}>iPhone без кнопки: Настройки → Календарь → Учётные записи → Новая → Другое → Подписной календарь → вставить ссылку. Google: «Другие календари» → «Добавить по URL».</p>
        <button className="link" onClick={async () => { await post('/calendar/feed/reset', {}); await feed.refresh(); toast('Старая ссылка больше не работает') }}>Сменить ссылку</button>
        <div style={{ height: 1, background: "var(--line)", margin: "6px 0" }} />
        <Btn kind="soft" block icon="clock" onClick={() => { setOpen(false); setBusyOpen(true) }}>{f.busy ? 'Моя занятость подключена' : 'И показывать мою занятость'}</Btn>
        <p className="caption muted" style={{ margin: 0 }}>При выборе времени занятые у тебя часы отметятся сами. Партнёр видит только «занято».</p>
      </div>}
    </Sheet>
  </>)
}

// «Моя занятость»: the private iCal link of my own calendar. Only busy times are read — the partner sees
// «занят(а)», never what it is.
function BusySheet({ open, onClose, connected, onDone }: { open: boolean; onClose: () => void; connected: boolean; onDone: () => void }) {
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try { const r = await post<{ busy: number }>('/calendar/busy', { url }); haptic.ok(); toast(`Подключено — вижу ${r.busy} занятых промежутков`); setUrl(''); onDone(); onClose() }
    catch { toast('Не получилось прочитать календарь — проверь, что это секретная ссылка iCal') } finally { setBusy(false) }
  }
  const off = async () => { await del('/calendar/busy'); onDone(); onClose(); toast('Отключено') }
  return (
    <Sheet open={open} onClose={onClose}>
      <h2 className="title" style={{ margin: '0 0 6px' }}>Моя занятость</h2>
      <p className="callout muted" style={{ margin: '0 0 12px' }}>Когда выбираете время, занятые у тебя часы отметятся сами, а «Мне подходит любое» их пропустит. {''}
        Партнёр увидит только «занято» — без названий и подробностей.</p>
      <div className="col" style={{ gap: 10 }}>
        <div className="field"><label>Секретная ссылка на календарь (iCal)</label>
          <input className="input" placeholder="https://calendar.google.com/calendar/ical/…/basic.ics" value={url} onChange={e => setUrl(e.target.value)} /></div>
        <Btn kind="accent" block busy={busy} disabled={!/^(https|webcal):\/\//.test(url.trim())} onClick={save}>Подключить</Btn>
        <details className="howto"><summary>Где взять ссылку</summary>
          <p><b>Google:</b> calendar.google.com → ⚙ Настройки → слева твой календарь → «Секретный адрес в формате iCal» → скопировать.</p>
          <p><b>iPhone (iCloud):</b> приложение Календарь → «Календари» → ⓘ у нужного → «Общий календарь» включить → «Поделиться ссылкой» → скопировать.</p>
          <p><b>Яндекс:</b> Календарь → ⚙ у календаря → «Экспорт» → ссылка iCal.</p>
          <p><b>Outlook:</b> Настройки → Календарь → «Общие календари» → «Опубликовать» → ссылка ICS.</p>
        </details>
        {connected && <button className="link" onClick={off}>Отключить мою занятость</button>}
      </div>
    </Sheet>
  )
}
