import { plural } from '../lib/format'
import { useCallback, useMemo, useState } from 'react'
import type { Home, Idea, Person } from '../lib/types'
import { useLoad, useOn, emit } from '../lib/data'
import { go } from '../lib/router'
import { post, humanError } from '../lib/api'
import { Empty, IdeaTile, Segmented, Skel, SubHeader, toast, Btn } from '../ui/kit'
import { type TONES } from '../ui/Folder'
import { haptic } from '../lib/tg'
import { Afisha } from './Afisha'
import { Icon } from '../ui/icons'
import { Going } from '../ui/Going'

type Def = { id: string; label: string; tone: keyof typeof TONES; icon: React.ReactNode; test: (i: Idea, now: number) => boolean; always?: boolean; chores?: boolean }

const want = (w?: string) => w === 'want' || w === 'very'
const DAY = 864e5

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

type Filter = { id: string; label: string; test: (i: Idea, now: number) => boolean; empty: string }
export const FILTERS: Filter[] = [
  { id: 'both', label: 'Хотим оба', test: i => want(i.my_wish) && want(i.partner_wish), empty: 'Здесь появятся идеи, которые вы оба отметили «хочу».' },
  { id: 'unmarked', label: 'Не отмечено', test: i => !i.my_wish, empty: 'Всё отмечено — отличная работа.' },
  { id: 'new', label: 'Новое', test: (i, now) => now - i.created_at * 1000 < 7 * DAY, empty: 'За неделю ничего нового.' },
  { id: 'soon', label: 'Скоро закончится', test: (i, now) => !!i.ends_at && i.ends_at * 1000 - now < 14 * DAY, empty: 'Ничего не заканчивается в ближайшие две недели.' },
  { id: 'liked', label: 'Повторить', test: i => !!i.liked_before, empty: 'Здесь будут места, куда вы хотите вернуться.' },
]

export function Ideas({ home, folder }: { home: Home | null; folder?: string }) {
  const all = useLoad<{ ideas: Idea[] }>('/ideas', 25000)
  const arch = useLoad<{ ideas: Idea[] }>('/ideas?status=archived')
  const refresh = useCallback(() => { all.refresh(); arch.refresh() }, [all.refresh, arch.refresh])
  useOn('ideas', refresh)
  const [q, setQ] = useState('')
  // a filter is local state (no route change, so the screen does not re-animate); links like ideas/unmarked preselect it
  const [flt, setFlt] = useState(FILTERS.some(f => f.id === folder) ? folder! : '')
  // two views of one place: our ideas and the afisha (what the scout and the channels found) — a switch, not a route
  const [view, setView] = useState<'ours' | 'afisha'>(folder === 'scout' ? 'afisha' : 'ours')
  const now = Date.now()
  // «Наши идеи» — only what we put here ourselves: added by one of us, or something from the afisha one of us
  // marked «хочу». The rest of what the scout and the channels found lives in the afisha, not in the bank.
  const ideas = useMemo(() => (all.data?.ideas || []).filter(ours), [all.data])
  const leisure = ideas.filter(i => i.kind === 'leisure')
  const found = useMemo(() => q.trim() ? ideas.filter(i => (i.title + ' ' + (i.address || '') + ' ' + (i.note || '')).toLowerCase().includes(q.trim().toLowerCase())) : [], [q, ideas])

  const top = (<>
    <header className="hdr"><h1 className="display">Куда пойти</h1><span className="spacer" />{view === 'ours' && <span className="pill">{`${leisure.length} ${plural(leisure.length, 'идея', 'идеи', 'идей')}`}</span>}</header>
    <div style={{ marginBottom: 12 }}><Segmented<'ours' | 'afisha'> items={[['ours', 'Наши идеи'], ['afisha', '🔭 Афиша']]} value={view} onChange={v => { haptic.select(); setView(v) }} /></div>
  </>)
  if (view === 'afisha' && (!folder || folder === 'scout')) return <>{top}<Afisha embedded /></>
  if (!all.data || !home) return <><Skel h={44} /><div className="folders">{[0, 1, 2, 3, 4, 5].map(k => <Skel key={k} h={130} />)}</div></>
  if (folder && !FILTERS.some(f => f.id === folder)) {
    const def = FOLDERS.find(f => f.id === folder)
    const list = folder === 'archive' ? arch.data?.ideas || [] : def ? ideas.filter(i => def.test(i, now)) : []
    const title = folder === 'archive' ? 'Архив' : def?.label || 'Папка'
    return (<>
      <SubHeader title={title} right={<span className="pill">{list.length}</span>} />
      {def?.chores ? <Chores list={list} me={home.me.color} /> :
        list.length === 0 ? <Empty title="Пусто" text="В этой папке пока нет идей." />
          : <div className="grid">{list.map((i, k) => <IdeaTile key={i.id} idea={i} me={home.me} d={k} onOpen={() => go('idea/' + i.id)} />)}</div>}
    </>)
  }
  const counts = Object.fromEntries(FILTERS.map(f => [f.id, leisure.filter(i => f.test(i, now)).length]))
  const chips = FILTERS.filter(f => counts[f.id] > 0 || f.id === flt)
  const active = FILTERS.find(f => f.id === flt)
  const filtered = active ? leisure.filter(i => active.test(i, now)) : []
  const pick = (id: string) => { haptic.select(); setFlt(f => f === id ? '' : id) }
  return (
    <>
      {top}
      <label className="searchbar"><Icon n="search" s={19} /><input placeholder="Найти в копилке" value={q} onChange={e => setQ(e.target.value)} />{q && <button className="iconbtn" style={{ width: 28, height: 28, boxShadow: 'none' }} onClick={() => setQ('')} aria-label="Очистить"><Icon n="x" s={14} /></button>}</label>
      {!q.trim() && leisure.length > 0 && (
        <div className="wkchips fchips" role="tablist" aria-label="Показать">
          <button className="wkchip" role="tab" aria-selected={!flt} data-on={!flt || undefined} onClick={() => { haptic.select(); setFlt('') }}>Всё</button>
          {chips.map(f => <button key={f.id} className="wkchip" role="tab" aria-selected={flt === f.id} data-on={flt === f.id || undefined} onClick={() => pick(f.id)}>
            {f.id === 'both' && <Icon n="heart" s={13} w={2.4} />}{f.label}<b>{counts[f.id]}</b></button>)}
        </div>
      )}
      {q.trim() ? (
        <div style={{ marginTop: 14 }}>{found.length === 0 ? <Empty title="Ничего не нашёл" text="Попробуй другое слово — или добавь эту идею." /> :
          <div className="grid">{found.map((i, k) => <IdeaTile key={i.id} idea={i} me={home.me} d={k} onOpen={() => go('idea/' + i.id)} />)}</div>}</div>
      ) : ideas.length === 0 ? (
        <Empty title="Копилка пуста" text="Пришли боту ссылку на ресторан, выставку или просто напиши идею — она появится здесь." />
      ) : active ? (
        filtered.length === 0 ? <Empty title="Пусто" text={active.empty} /> :
          <div className="grid" key={active.id}>{filtered.map((i, k) => <IdeaTile key={i.id} idea={i} me={home.me} d={k} onOpen={() => go('idea/' + i.id)} />)}</div>
      ) : (<>
        <Going list={home.going || []} />
        <Rail title="♥ Хотите оба" items={ideas.filter(i => FILTERS[0].test(i, now)).slice(0, 10)} me={home.me} more={counts.both > 0 ? () => pick('both') : undefined} testid="rail-both" />
        <NewRail me={home.me} onAll={() => setView('afisha')} />
        {(counts.unmarked || 0) > 3 && <button className="triage-cta rise" onClick={() => go('triage')}><Icon n="folder" s={20} /><span><b>Разобрать неотмеченное · {counts.unmarked}</b><small>По одной карточке — минут на пять</small></span><Icon n="chev" s={16} w={2.4} /></button>}
        <div className="sec"><h2 className="title"><span className="orange-ind" />Ваши идеи · {leisure.length}</h2><button className="link" onClick={() => go('ideas/chores')}>Дела</button></div>
        <div className="wkchips fchips">{FOLDERS.filter(f => !f.chores).map(f => { const n = ideas.filter(i => f.test(i, now)).length; return n ? <button key={f.id} className="wkchip" onClick={() => go('ideas/' + f.id)}>{f.label}<b>{n}</b></button> : null })}</div>
        <div className="grid">{[...leisure].sort(byUse).map((i, k) => <IdeaTile key={i.id} idea={i} me={home.me} d={k} onOpen={() => go('idea/' + i.id)} />)}</div>
        {!!arch.data?.ideas.length && <button className="archrow" onClick={() => go('ideas/archive')}><Icon n="trash" s={17} />Архив<span className="spacer" /><span className="muted">{arch.data.ideas.length}</span><Icon n="chev" s={15} w={2.2} /></button>}
      </>)}
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

function Rail({ title, items, me, more, testid }: { title: string; items: Idea[]; me: Person; more?: () => void; testid?: string }) {
  if (!items.length) return null
  return (<>
    <div className="sec"><h2 className="title">{title}</h2>{more && <button className="link" onClick={more}>Все</button>}</div>
    <div className="rail" data-testid={testid}>{items.map((i, k) => <div key={i.id} style={{ width: 200 }}><IdeaTile idea={i} me={me} d={k} onOpen={() => go('idea/' + i.id)} /></div>)}</div>
  </>)
}

// what the scout and the channels found and neither of you has marked — the best for you two first
function NewRail({ me, onAll }: { me: Person; onAll: () => void }) {
  const res = useLoad<{ ideas: Idea[]; counts: Record<string, number> }>('/afisha?horizon=all&source=all&sort=match&fresh=1')
  const items = (res.data?.ideas || []).slice(0, 10)
  if (!items.length) return null
  return (<>
    <div className="sec"><h2 className="title">🆕 Новое для вас · {res.data?.counts.fresh ?? items.length}</h2><button className="link" onClick={onAll}>Вся афиша</button></div>
    <div className="rail" data-testid="rail-new">{items.map((i, k) => <div key={i.id} style={{ width: 200 }}><IdeaTile idea={i} me={me} d={k} onOpen={() => go('idea/' + i.id)} /></div>)}</div>
  </>)
}
