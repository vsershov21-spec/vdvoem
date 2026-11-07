import { plural } from '../lib/format'
import { useCallback, useMemo, useState } from 'react'
import type { Home, Idea } from '../lib/types'
import { useLoad, useOn, emit } from '../lib/data'
import { go } from '../lib/router'
import { post, humanError } from '../lib/api'
import { Empty, IdeaTile, Skel, SubHeader, toast, Btn } from '../ui/kit'
import { Folder, type TONES } from '../ui/Folder'
import { Heart } from '../ui/Heart'
import { Icon } from '../ui/icons'

type Def = { id: string; label: string; tone: keyof typeof TONES; icon: React.ReactNode; test: (i: Idea, now: number) => boolean; always?: boolean; chores?: boolean }

const want = (w?: string) => w === 'want' || w === 'very'
const DAY = 864e5

export const FOLDERS: Def[] = [
  { id: 'both', label: 'Хотим оба', tone: 'rose', icon: <Heart on={false} size={44} stroke={2.4} color="#fff" burst={false} />, test: i => i.kind === 'leisure' && want(i.my_wish) && want(i.partner_wish), always: true },
  { id: 'liked', label: 'Понравилось', tone: 'rose', icon: <Icon n="heart" s={40} w={2.4} />, test: i => !!i.liked_before },
  { id: 'soon', label: 'Скоро закончится', tone: 'gold', icon: <Icon n="clock" s={40} w={2.4} />, test: (i, now) => !!i.ends_at && i.ends_at * 1000 - now < 14 * DAY },
  { id: 'new', label: 'Новое', tone: 'ocean', icon: <Icon n="spark" s={40} w={2.4} />, test: (i, now) => now - i.created_at * 1000 < 7 * DAY && i.kind === 'leisure' },
  { id: 'unmarked', label: 'Не отмечено', tone: 'onyx', icon: <Icon n="edit" s={40} w={2.4} />, test: i => i.kind === 'leisure' && !i.my_wish },
  { id: 'food', label: 'Еда', tone: 'ember', icon: <Icon n="food" s={40} w={2.4} />, test: i => i.kind === 'leisure' && i.category === 'food' },
  { id: 'culture', label: 'Культура', tone: 'onyx', icon: <Icon n="culture" s={40} w={2.4} />, test: i => i.kind === 'leisure' && i.category === 'culture' },
  { id: 'walk', label: 'Прогулки', tone: 'forest', icon: <Icon n="walk" s={40} w={2.4} />, test: i => i.kind === 'leisure' && i.category === 'walk' },
  { id: 'sport', label: 'Спорт', tone: 'ocean', icon: <Icon n="sport" s={40} w={2.4} />, test: i => i.kind === 'leisure' && i.category === 'sport' },
  { id: 'other', label: 'Другое', tone: 'gold', icon: <Icon n="other" s={40} w={2.4} />, test: i => i.kind === 'leisure' && (i.category === 'other' || i.category === 'home') },
  { id: 'chores', label: 'Дела', tone: 'ocean', icon: <Icon n="chore" s={40} w={2.4} />, test: i => i.kind === 'chore', always: true, chores: true },
]

export function Ideas({ home, folder }: { home: Home | null; folder?: string }) {
  const all = useLoad<{ ideas: Idea[] }>('/ideas', 25000)
  const arch = useLoad<{ ideas: Idea[] }>('/ideas?status=archived')
  const refresh = useCallback(() => { all.refresh(); arch.refresh() }, [all.refresh, arch.refresh])
  useOn('ideas', refresh)
  const [q, setQ] = useState('')
  const now = Date.now()
  const ideas = all.data?.ideas || []
  const found = useMemo(() => q.trim() ? ideas.filter(i => (i.title + ' ' + (i.address || '') + ' ' + (i.note || '')).toLowerCase().includes(q.trim().toLowerCase())) : [], [q, ideas])

  if (!all.data || !home) return <><Skel h={44} /><div className="folders">{[0, 1, 2, 3, 4, 5].map(k => <Skel key={k} h={130} />)}</div></>
  if (folder) {
    const def = FOLDERS.find(f => f.id === folder)
    const list = folder === 'archive' ? arch.data?.ideas || [] : def ? ideas.filter(i => def.test(i, now)) : []
    const title = folder === 'archive' ? 'Архив' : def?.label || 'Папка'
    return (<>
      <SubHeader title={title} right={<span className="pill">{list.length}</span>} />
      {def?.chores ? <Chores list={list} me={home.me.color} /> :
        list.length === 0 ? <Empty title="Пусто" text={folder === 'both' ? 'Здесь появятся идеи, которые вы оба отметили «хочу».' : 'В этой папке пока нет идей.'} />
          : <div className="grid">{list.map((i, k) => <IdeaTile key={i.id} idea={i} me={home.me} d={k} onOpen={() => go('idea/' + i.id)} />)}</div>}
    </>)
  }
  const shown = FOLDERS.map(f => ({ f, list: ideas.filter(i => f.test(i, now)) })).filter(x => x.f.always || x.list.length > 0)
  return (
    <>
      <header className="hdr"><h1 className="display">Копилка</h1><span className="spacer" /><span className="pill">{(n => `${n} ${plural(n, 'идея', 'идеи', 'идей')}`)(ideas.filter(i => i.kind === 'leisure').length)}</span></header>
      <label className="searchbar"><Icon n="search" s={19} /><input placeholder="Найти в копилке" value={q} onChange={e => setQ(e.target.value)} />{q && <button className="iconbtn" style={{ width: 28, height: 28, boxShadow: 'none' }} onClick={() => setQ('')} aria-label="Очистить"><Icon n="x" s={14} /></button>}</label>
      {q.trim() ? (
        <div style={{ marginTop: 14 }}>{found.length === 0 ? <Empty title="Ничего не нашёл" text="Попробуй другое слово — или добавь эту идею." /> :
          <div className="grid">{found.map((i, k) => <IdeaTile key={i.id} idea={i} me={home.me} d={k} onOpen={() => go('idea/' + i.id)} />)}</div>}</div>
      ) : ideas.length === 0 ? (
        <Empty title="Копилка пуста" text="Пришли боту ссылку на ресторан, выставку или просто напиши идею — она появится здесь." />
      ) : (
        <div className="folders">
          {shown.map(({ f, list }, k) => <Folder key={f.id} d={k} label={f.label} count={list.length} ideas={list.slice(0, 3)} tone={f.tone} icon={f.icon} onOpen={() => go('ideas/' + f.id)} />)}
          {!!arch.data?.ideas.length && <Folder label="Архив" count={arch.data.ideas.length} ideas={arch.data.ideas.slice(0, 3)} tone="onyx" icon={<Icon n="trash" s={40} w={2.4} />} onOpen={() => go('ideas/archive')} d={shown.length} />}
        </div>
      )}
    </>
  )
}

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
