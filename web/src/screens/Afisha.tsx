// «Афиша» — what the scout found: the week ahead and announcements 1–3 months out, ranked by how much
// you both would like it (each person's own learned taste). Images load straight from the listing sites.
import { useMemo, useState } from 'react'
import type { Idea, Wish } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, humanError } from '../lib/api'
import { go } from '../lib/router'
import { haptic } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Btn, Empty, Segmented, Sheet, Skel, SubHeader, toast } from '../ui/kit'
import { EventCard } from '../ui/EventCard'
import { MonthGrid, type DayMark } from '../ui/Charts'

const ym = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
const ymdOf = (unix: number) => { const d = new Date(unix * 1000); return `${ym(d)}-${String(d.getDate()).padStart(2, '0')}` }

type KV = { k: string; n: number }
type Res = { ideas: Idea[]; counts: Record<string, number>; genres: KV[]; cats: KV[] }
type F = { hz: 'all' | 'week' | 'later' | 'ending'; source: 'all' | 'scout' | 'channel' | 'ours'; sort: 'match' | 'date' | 'price'; fresh: boolean; genre: string; cat: string }
const SRC: [F['source'], string][] = [['all', 'Все'], ['scout', '🔭 Скаут'], ['channel', '📣 Каналы'], ['ours', '🗂 Наши']]
const load = (): F => { try { return { hz: 'all', source: 'all', sort: 'match', fresh: false, genre: '', cat: '', ...JSON.parse(localStorage.getItem('vd-afisha') || '{}') } } catch { return { hz: 'all', source: 'all', sort: 'match', fresh: false, genre: '', cat: '' } } }


// one row in sight (when), everything else behind «Фильтры» — the first event is on the first screen
export function Afisha({ embedded }: { embedded?: boolean }) {
  const [f, setF] = useState<F>(load)
  const [more, setMore] = useState(false)
  const [byDay, setByDay] = useState(false)
  const upd = (p: Partial<F>) => { haptic.select(); setF(x => { const n = { ...x, ...p }; try { localStorage.setItem('vd-afisha', JSON.stringify(n)) } catch { /* private */ } return n }) }
  const qs = new URLSearchParams({ horizon: f.hz, source: f.source, sort: f.hz === 'ending' ? 'date' : f.sort, ...(f.fresh ? { fresh: '1' } : {}), ...(f.genre ? { genre: f.genre } : {}), ...(f.cat ? { cat: f.cat } : {}) })
  const res = useLoad<Res>('/afisha?' + qs)
  const mark = async (i: Idea, w: Wish) => {
    haptic.tap()
    try {
      await post('/ideas/' + i.id + '/wish', { wish: w })
      res.set(r => r && { ...r, ideas: r.ideas.map(x => x.id === i.id ? { ...x, my_wish: w } : x) })
      emit('ideas'); emit('home')
      if (w !== 'skip') toast(w === 'very' ? 'Очень хочу ✓ — в копилке' : 'Хочу ✓ — в наших идеях')
    } catch (e) { toast(humanError(e)) }
  }
  const counts = res.data?.counts || {}
  const n = (k: string) => counts[k] != null ? <b>{counts[k]}</b> : null
  const extra = (f.source !== 'all' ? 1 : 0) + (f.fresh ? 1 : 0) + (f.sort !== 'match' ? 1 : 0) + (f.genre ? 1 : 0)
  return (<>
    {!embedded && <SubHeader title="Афиша" />}
    <div className="row" style={{ justifyContent: 'flex-end', margin: '-4px 0 8px' }}>
      <button className="link" data-testid="afisha-view" onClick={() => { haptic.select(); setByDay(x => !x) }}><Icon n={byDay ? 'folder' : 'cal'} s={14} />{byDay ? 'Списком' : 'По дням'}</button>
    </div>
    {byDay ? <AfishaMonth /> : <>
    <div className="wkchips fchips" role="tablist" aria-label="Когда">
      {([['all', 'Всё'], ['week', 'Ближайшие 2 недели'], ['ending', '⏳ Скоро закончатся'], ['later', 'Анонсы']] as [F['hz'], string][]).filter(([k]) => k === 'all' || k === f.hz || counts[k] !== 0).map(([k, l]) =>
        <button key={k} className="wkchip" role="tab" aria-selected={f.hz === k} data-on={f.hz === k || undefined} onClick={() => upd({ hz: k })}>{l}{n(k)}</button>)}
      <button className="wkchip" data-on={extra > 0 || undefined} onClick={() => setMore(true)} aria-label="Фильтры"><Icon n="filter" s={13} w={2.4} />Фильтры{extra > 0 && <b>{extra}</b>}</button>
    </div>
    <Sheet open={more} onClose={() => setMore(false)}>
      <h2 className="title" style={{ margin: '0 0 12px' }}>Фильтры</h2>
      <p className="caption muted" style={{ margin: '0 0 6px' }}>Откуда</p>
      <div className="wkchips" aria-label="Источник" style={{ flexWrap: 'wrap' }}>
        {SRC.filter(([k]) => k === 'all' || k === f.source || counts[k] !== 0).map(([k, l]) => <button key={k} className="wkchip" data-on={f.source === k || undefined} aria-pressed={f.source === k} onClick={() => upd({ source: k })}>{l}{k !== 'all' ? n(k) : null}</button>)}
        {(f.fresh || counts.fresh !== 0) && <button className="wkchip" data-on={f.fresh || undefined} aria-pressed={f.fresh} onClick={() => upd({ fresh: !f.fresh })}>🆕 Ещё не видели{n('fresh')}</button>}
      </div>
      <p className="caption muted" style={{ margin: '14px 0 6px' }}>Порядок</p>
      <Segmented<F['sort']> items={[['match', 'По интересам'], ['date', 'По дате'], ['price', 'Дешевле']]} value={f.sort} onChange={v => upd({ sort: v })} />
      {!!res.data?.genres?.length && <>
        <p className="caption muted" style={{ margin: '14px 0 6px' }}>Что именно</p>
        <div className="wkchips" aria-label="Жанр" style={{ flexWrap: 'wrap' }}>
          {f.genre && <button className="wkchip" onClick={() => upd({ genre: '' })}><Icon n="x" s={12} />Любое</button>}
          {res.data.genres.map(g => <button key={g.k} className="wkchip" data-on={f.genre === g.k || undefined} onClick={() => upd({ genre: f.genre === g.k ? '' : g.k })}>{g.k}<b>{g.n}</b></button>)}
        </div>
      </>}
      <div className="row" style={{ marginTop: 18, gap: 8 }}>
        {extra > 0 && <Btn kind="soft" block onClick={() => upd({ source: 'all', fresh: false, sort: 'match', genre: '' })}>Сбросить</Btn>}
        <Btn kind="primary" block onClick={() => setMore(false)}>Показать{res.data ? ' · ' + res.data.ideas.length : ''}</Btn>
      </div>
    </Sheet>
    <div className="row" style={{ margin: '2px 2px 10px', gap: 8 }}>
      <span className="caption muted" style={{ flex: 1 }}>{res.data ? `${res.data.ideas.length} ${res.data.ideas.length % 10 === 1 && res.data.ideas.length % 100 !== 11 ? 'событие' : 'событий'}` : ' '}{f.sort === 'match' ? ' · сверху то, что понравится вам обоим' : ''}</span>
      <button className="link" onClick={() => go('assistant')}><Icon n="spark" s={14} />Спросить</button>
    </div>
    {!res.data ? <div className="col">{[0, 1, 2].map(k => <Skel key={k} h={150} />)}</div>
      : res.data.ideas.length === 0 ? <Empty title="Под эти фильтры пусто" text={f.genre || f.fresh || f.source !== 'all' || f.hz !== 'all'
        ? 'Сними фильтр — или скажи помощнику «следи за …», и скаут поищет специально.'
        : 'Скаут приносит события под ваши вкусы, каналы — посты, которые ты пересылаешь боту.'} />
      : <div className="col">{res.data.ideas.map((i, k) => <EventCard key={i.id} i={i} k={k} onMark={mark} />)}</div>}
    </>}
  </>)
}

// «По дням»: dots for days with events (the number of them), a tap on a day lists them; «Хочу» takes one
// into our bank. Only events with a date — exhibitions running for weeks live in the afisha's «Скоро закончатся».
function AfishaMonth() {
  const [month, setMonth] = useState(() => { const d = new Date(); return new Date(d.getFullYear(), d.getMonth(), 1) })
  const today = ymdOf(Date.now() / 1000)
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
      if (w !== 'skip') toast(w === 'very' ? 'Очень хочу ✓ — в наших идеях' : 'Хочу ✓ — в наших идеях')
    } catch (e) { toast(humanError(e)) }
  }
  return (<>
    {!res.data ? <Skel h={330} r={24} /> : <div className="rise">
      <MonthGrid month={month} marks={marks} today={today} selected={sel} legend={[['#f0b400', 'события дня']]}
        onSelect={d => setSel(s => s === d ? '' : d)} onMonth={n => { setSel(''); setMonth(m => new Date(m.getFullYear(), m.getMonth() + n, 1)) }} />
    </div>}
    <div className="sec"><h2 className="title"><span className="orange-ind" />{sel ? new Date(sel + 'T12:00').toLocaleDateString('ru', { weekday: 'long', day: 'numeric', month: 'long' }) : 'Афиша месяца'}</h2>
      {sel && <button className="link" onClick={() => setSel('')}>Все</button>}</div>
    {!res.data ? null : list.length === 0
      ? <div className="card"><Empty title={sel ? 'В этот день событий нет' : 'В этом месяце событий с датой нет'} text="Скаут приносит события каждый день — или спроси помощника «найди новое на …»." /></div>
      : <div className="col">{list.map((i, k) => <EventCard key={i.id} i={i} k={k} onMark={mark} />)}</div>}
  </>)
}


