import { useCallback, useMemo, useState } from 'react'
import type { Home, Idea } from '../lib/types'
import { useLoad, useOn, emit } from '../lib/data'
import { go } from '../lib/router'
import { post, humanError } from '../lib/api'
import { Cover, Empty, IdeaTile, Segmented, Skel, SubHeader, toast, Btn } from '../ui/kit'
import { startWith } from '../lib/startWith'
import { type TONES } from '../ui/Folder'
import { haptic } from '../lib/tg'
import { Afisha } from './Afisha'
import { Icon } from '../ui/icons'

type Def = { id: string; label: string; tone: keyof typeof TONES; icon: React.ReactNode; test: (i: Idea, now: number) => boolean; always?: boolean; chores?: boolean }

const want = (w?: string) => w === 'want' || w === 'very'

// Each idea lives in exactly one folder — its category (or «Дела»). Cross-cutting views
// («Хотим оба», «Не отмечено», «Новое»…) are filters on top, not folders, so nothing repeats.
export const FOLDERS: Def[] = [
  { id: 'food', label: 'Еда', tone: 'ember', icon: <Icon n="food" s={40} w={2.4} />, test: i => i.kind === 'leisure' && i.category === 'food' },
  { id: 'culture', label: 'Культура', tone: 'onyx', icon: <Icon n="culture" s={40} w={2.4} />, test: i => i.kind === 'leisure' && i.category === 'culture' },
  { id: 'walk', label: 'Прогулки', tone: 'forest', icon: <Icon n="walk" s={40} w={2.4} />, test: i => i.kind === 'leisure' && i.category === 'walk' },
  { id: 'sport', label: 'Спорт', tone: 'ocean', icon: <Icon n="sport" s={40} w={2.4} />, test: i => i.kind === 'leisure' && i.category === 'sport' },
  { id: 'other', label: 'Другое', tone: 'gold', icon: <Icon n="other" s={40} w={2.4} />, test: i => i.kind === 'leisure' && i.category !== 'food' && i.category !== 'culture' && i.category !== 'walk' && i.category !== 'sport' },
  { id: 'chores', label: 'Дела', tone: 'rose', icon: <Icon n="chore" s={40} w={2.4} />, test: i => i.kind === 'chore', always: true, chores: true },
]

type View = 'ours' | 'afisha' | 'both'

export function Ideas({ home, folder }: { home: Home | null; folder?: string }) {
  const all = useLoad<{ ideas: Idea[] }>('/ideas', 25000)
  const arch = useLoad<{ ideas: Idea[] }>('/ideas?status=archived')
  const refresh = useCallback(() => { all.refresh(); arch.refresh() }, [all.refresh, arch.refresh])
  useOn('ideas', refresh)
  const [q, setQ] = useState('')
  // a filter is local state (no route change, so the screen does not re-animate); links like ideas/unmarked preselect it
  const [cat, setCat] = useState('') // a category filters the grid in place
  // two views of one place: our ideas and the afisha (what the scout and the channels found) — a switch, not a route
  const [view, setView] = useState<View>(folder === 'scout' ? 'afisha' : folder === 'both' ? 'both' : 'ours')
  const now = Date.now()
  // «Наши идеи» — only what we put here ourselves: added by one of us, or something from the afisha one of us
  // marked «хочу». The rest of what the scout and the channels found lives in the afisha, not in the bank.
  const ideas = useMemo(() => (all.data?.ideas || []).filter(ours), [all.data])
  const leisure = ideas.filter(i => i.kind === 'leisure')
  const found = useMemo(() => q.trim() ? ideas.filter(i => (i.title + ' ' + (i.address || '') + ' ' + (i.note || '')).toLowerCase().includes(q.trim().toLowerCase())) : [], [q, ideas])

  const both = leisure.filter(i => want(i.my_wish) && want(i.partner_wish)).sort(byUse)
  const top = (<>
    <header className="hdr"><h1 className="display">Идеи</h1></header>
    <div style={{ marginBottom: 12 }}><Segmented<View> items={[['ours', 'Наши'], ['afisha', 'Афиша'], ['both', both.length ? `Совпало · ${both.length}` : 'Совпало']]} value={view} onChange={v => { haptic.select(); setView(v) }} /></div>
  </>)
  const single = folder && folder !== 'scout' && folder !== 'both'
  if (view === 'afisha' && !single) return <>{top}<Afisha embedded /></>
  if (!all.data || !home) return <><Skel h={44} /><Skel h={44} /><div className="grid">{[0, 1, 2, 3].map(k => <Skel key={k} h={150} />)}</div></>
  if (single) {
    const def = FOLDERS.find(f => f.id === folder)
    const list = folder === 'archive' ? arch.data?.ideas || [] : folder === 'liked' ? ideas.filter(i => i.liked_before) : def ? ideas.filter(i => def.test(i, now)) : []
    const title = folder === 'archive' ? 'Архив' : folder === 'liked' ? 'Понравилось' : def?.label || 'Идеи'
    return (<>
      <SubHeader title={title} back="Идеи" right={<span className="pill">{list.length}</span>} />
      {def?.chores ? <Chores list={list} me={home.me.color} /> :
        list.length === 0 ? <Empty title="Пусто" />
          : <div className="grid">{list.map((i, k) => <IdeaTile key={i.id} idea={i} me={home.me} d={k} onOpen={() => go('idea/' + i.id)} />)}</div>}
    </>)
  }
  if (view === 'both') return (<>
    {top}
    {both.length === 0 ? <Empty title="Совпадений пока нет" text="Совпадение — когда «хочу» отметили оба."><Btn kind="primary" icon="heart" onClick={() => go('')}>Свайпать</Btn></Empty> : (
      <div className="col" style={{ gap: 8 }} data-testid="matches">
        {both.map(i => (
          <div key={i.id} className="pickrow rise">
            <button className="pickrow__open" onClick={() => go('idea/' + i.id)} aria-label={'Открыть: ' + i.title}>
              <span className="pickrow__img"><Cover idea={i} h={64}><span /><span /></Cover></span>
              <span className="pickrow__main"><b>{i.title}</b><span>{i.meta}</span></span>
            </button>
            <Btn sm kind="primary" onClick={() => { startWith(i.id, i.title) }}>Идём</Btn>
          </div>))}
      </div>)}
  </>)
  const cats = FOLDERS.filter(f => !f.chores).map(f => [f, leisure.filter(i => f.test(i, now)).length] as const).filter(([, n]) => n > 0)
  const shown = (cat ? leisure.filter(i => FOLDERS.find(f => f.id === cat)!.test(i, now)) : [...leisure]).sort(byUse)
  const nChores = ideas.filter(i => i.kind === 'chore').length
  return (
    <>
      {top}
      <label className="searchbar"><Icon n="search" s={19} /><input placeholder="Найти" value={q} onChange={e => setQ(e.target.value)} />{q && <button className="iconbtn" style={{ width: 28, height: 28, boxShadow: 'none' }} onClick={() => setQ('')} aria-label="Очистить"><Icon n="x" s={14} /></button>}</label>
      {q.trim() ? (
        <div style={{ marginTop: 14 }}>{found.length === 0 ? <Empty title="Не нашлось" /> :
          <div className="grid">{found.map((i, k) => <IdeaTile key={i.id} idea={i} me={home.me} d={k} onOpen={() => go('idea/' + i.id)} />)}</div>}</div>
      ) : leisure.length === 0 ? (
        <Empty title="Своих идей пока нет" text="Пришли боту ссылку или название — появится здесь.">
          <Btn kind="primary" icon="spark" onClick={() => { haptic.select(); setView('afisha') }}>Смотреть афишу</Btn>
        </Empty>
      ) : (<>
        {cats.length > 1 && <div className="wkchips fchips" role="tablist" aria-label="Показать">
          <button className="wkchip" role="tab" aria-selected={!cat} data-on={!cat || undefined} onClick={() => { haptic.select(); setCat('') }}>Все<b>{leisure.length}</b></button>
          {cats.map(([f, n]) => <button key={f.id} className="wkchip" role="tab" aria-selected={cat === f.id} data-on={cat === f.id || undefined} onClick={() => { haptic.select(); setCat(c => c === f.id ? '' : f.id) }}>{f.label}<b>{n}</b></button>)}
        </div>}
        <div className="grid" key={cat}>{shown.map((i, k) => <IdeaTile key={i.id} idea={i} me={home.me} d={k} onOpen={() => go('idea/' + i.id)} />)}</div>
      </>)}
      {!q.trim() && <div className="list" style={{ marginTop: 16 }}>
        <button className="li" onClick={() => go('ideas/chores')}><span className="li__ico"><Icon n="chore" s={18} /></span><span className="li__main"><b>Дела</b></span><span className="muted">{nChores || ''}</span><Icon n="chev" s={15} w={2.2} /></button>
        {!!arch.data?.ideas.length && <button className="li" onClick={() => go('ideas/archive')}><span className="li__ico"><Icon n="trash" s={18} /></span><span className="li__main"><b>Архив</b></span><span className="muted">{arch.data.ideas.length}</span><Icon n="chev" s={15} w={2.2} /></button>}
      </div>}
    </>
  )
}

const ours = (i: Idea) => i.ours !== false

function Chores({ list, me }: { list: Idea[]; me: string }) {
  const [done, setDone] = useState<Record<number, boolean>>({})
  const tick = async (i: Idea) => {
    setDone(d => ({ ...d, [i.id]: true }))
    try { await post(`/ideas/${i.id}/done`); setTimeout(() => { emit('ideas'); emit('home') }, 600) } catch (e) { toast(humanError(e)) }
  }
  if (!list.length) return <Empty title="Дел нет" text="Напиши боту «купить продукты» или «записаться к врачу» — дело появится здесь." />
  return (
    <div className="list">
      {list.map(i => (
        <div key={i.id} className="li" style={{ cursor: 'default', opacity: done[i.id] ? .45 : 1, transition: 'opacity .4s' }}>
          <button className="li__ico" aria-label="Сделано" onClick={() => tick(i)} style={{ border: 0, cursor: 'pointer', background: done[i.id] ? 'var(--success)' : 'var(--surface-sunken)', color: '#fff' }}>
            {done[i.id] && <Icon n="check" s={18} w={2.8} />}
          </button>
          <span className="li__main" onClick={() => go('idea/' + i.id)} style={{ cursor: 'pointer' }}>
            <b style={{ textDecoration: done[i.id] ? 'line-through' : undefined }}>{i.title}</b>
            <span>{i.assignee ? (i.assignee === me ? 'делаю я' : 'делает партнёр') : 'кто угодно'}{i.deadline ? ' · до ' + new Date(i.deadline * 1000).toLocaleDateString('ru', { day: 'numeric', month: 'short' }) : ''}</span>
          </span>
        </div>
      ))}
      <div style={{ padding: 12 }}><Btn kind="soft" block sm icon="plus" onClick={() => go('assistant')}>Спросить, что сделать на неделе</Btn></div>
    </div>
  )
}

// the bank, most useful first: what has a date soon, then what you both want, then what one of you wants
function byUse(a: Idea, b: Idea) {
  const now = Date.now() / 1000, soon = (i: Idea) => i.starts_at && i.starts_at > now && i.starts_at - now < 21 * 86400 ? i.starts_at : 9e12
  const w = (x?: string) => x === 'very' ? 2 : x === 'want' ? 1 : 0, score = (i: Idea) => w(i.my_wish) + w(i.partner_wish)
  return soon(a) - soon(b) || score(b) - score(a) || b.created_at - a.created_at
}
