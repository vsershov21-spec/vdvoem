import { useState } from 'react'
import { useLoad, useOn, emit } from '../lib/data'
import { post, patch, humanError, ApiError } from '../lib/api'
import { go, back } from '../lib/router'
import { haptic } from '../lib/tg'
import { CATS, money, when as whenText, ncase } from '../lib/format'
import type { Idea, Round } from '../lib/types'
import type { HomeV2 } from './types'
import { Sheet, Pic, Empty, say } from './ui'

const wants = (w?: string) => w === 'want' || w === 'very'
const matched = (i: Idea) => wants(i.my_wish) && wants(i.partner_wish)
const collecting = (i: Idea) => !i.parsed && !!i.url
const dayOf = (t: number) => { const d = new Date(t * 1000); return d.getMonth() * 100 + d.getDate() }

// «Идём» and «Позвать» lead to the same place: the meeting card of this idea
export async function goMeet(i: { id: number }) {
  try { const r = await post<{ round: Round }>('/rounds/with/' + i.id); emit('home'); go('meet/' + r.round.id) }
  catch (e) {
    if (e instanceof ApiError && e.code === 'round_open') {
      try { const r = await post<{ round: Round }>('/rounds/with/' + i.id + '?replace=1'); emit('home'); go('meet/' + r.round.id); return } catch (x) { say(humanError(x)); return }
    }
    say(humanError(e))
  }
}

// ---------- the tab ----------
type View = 'ours' | 'afisha' | 'both'
let lastView: View = 'ours'

export function Ideas({ home, onAdd }: { home: HomeV2 | null; onAdd: () => void }) {
  const { data, refresh } = useLoad<{ ideas: Idea[] }>('/ideas', 25000)
  useOn('ideas', refresh)
  const [view, setViewRaw] = useState<View>(lastView)
  const [who, setWho] = useState<'all' | 'mine' | 'their'>('all')
  const [cat, setCat] = useState('')
  const [byDay, setByDay] = useState(false)
  const [day, setDay] = useState(0)
  const setView = (v: View) => { lastView = v; setViewRaw(v) }
  const paired = !!home?.paired
  const pn = home?.partner?.name || 'партнёр'
  const all = (data?.ideas || []).filter(i => i.kind !== 'chore')
  const ours = all.filter(i => i.ours), afisha = all.filter(i => !i.ours), both = all.filter(matched)
  const v: View = !paired && view === 'both' ? 'ours' : view
  const badge = (i: Idea) => collecting(i) ? 'скаут собирает' : matched(i) ? 'совпало' : i.mine && !i.price_min ? 'нет цены' : ''
  const cats = [...new Set(afisha.map(i => i.category))].filter(c => CATS[c])
  const now = new Date(), first = new Date(now.getFullYear(), now.getMonth(), 1), days = new Date(now.getFullYear(), now.getMonth() + 1, 0).getDate()
  const shownAf = afisha.filter(i => (!cat || i.category === cat) && (!byDay || !day || (i.starts_at && dayOf(i.starts_at) === now.getMonth() * 100 + day)))
  const oursList = ours.filter(i => who === 'all' || (who === 'mine') === i.mine)
  return (
    <div className="scr" data-testid="v2-ideas">
      <div className="v-row"><h2>Идеи</h2><span className="sp" /><button className="b p s" data-testid="add-idea" onClick={onAdd}>＋ Идея</button></div>
      <div className="v-seg" role="tablist">
        {([['ours', 'Наши'], ['afisha', 'Афиша'], ...(paired ? [['both', 'Совпало · ' + both.length]] : [])] as [View, string][]).map(([k, l]) =>
          <button key={k} role="tab" aria-selected={v === k} className={v === k ? 'on' : ''} onClick={() => setView(k)}>{l}</button>)}
      </div>
      {!data ? null : v === 'both' ? (both.length ? both.map(i =>
        <div className="in" key={i.id} data-testid="match-row"><Pic idea={i} className="th" /><span style={{ cursor: 'pointer' }} onClick={() => go('idea/' + i.id)}><b>{i.title}</b><small>{[CATS[i.category]?.name, i.starts_at ? whenText(i.starts_at) : ''].filter(Boolean).join(' · ')}</small></span>
          <button className="b p s" onClick={() => goMeet(i)}>Идём</button></div>)
        : <Empty title="Пока не совпало"><button className="b p w" onClick={() => go('')}>Отмечать идеи</button></Empty>)
        : v === 'afisha' ? <>
          <div className="v-row"><div className="v-chips">
            <button className={'v-chip' + (!cat ? ' on' : '')} onClick={() => setCat('')}>Всё</button>
            {cats.map(c => <button key={c} className={'v-chip' + (cat === c ? ' on' : '')} onClick={() => setCat(c)}>{CATS[c].name}</button>)}
          </div><button className="back" style={{ flex: 'none' }} onClick={() => { setByDay(!byDay); setDay(0) }}>{byDay ? 'Списком' : 'По дням'}</button></div>
          {byDay && <div className="v-card"><b>{now.toLocaleDateString('ru', { month: 'long' })}</b><div className="cal">
            {['пн', 'вт', 'ср', 'чт', 'пт', 'сб', 'вс'].map(d => <span className="wd" key={d}>{d}</span>)}
            {Array.from({ length: (first.getDay() + 6) % 7 }, (_, k) => <span key={'e' + k} />)}
            {Array.from({ length: days }, (_, k) => { const d = k + 1, has = afisha.some(i => i.starts_at && dayOf(i.starts_at) === now.getMonth() * 100 + d)
              return <button key={d} className={(d === now.getDate() ? 'today ' : '') + (day === d ? 'sel' : '')} onClick={() => setDay(day === d ? 0 : d)}>{d}{has && <i className="d" />}</button> })}
          </div></div>}
          {shownAf.length ? shownAf.map(i => <button className="in" key={i.id} data-testid="afisha-row" onClick={() => go('idea/' + i.id)}><Pic idea={i} className="th" /><span><b>{i.title}</b><small>{[i.starts_at ? whenText(i.starts_at) : '', i.price_min ? money(i.price_min) : ''].filter(Boolean).join(' · ')}</small></span>{matched(i) ? <span className="badge">совпало</span> : wants(i.my_wish) ? '♥' : '›'}</button>)
            : <Empty title={day ? 'В этот день событий нет' : 'Афиша пока пуста'}><p className="m">Скаут ищет события каждый день.</p></Empty>}
        </> : <>
          {paired && <div className="v-chips">{([['all', 'Все'], ['mine', 'Мои'], ['their', ncase(pn, 'gen')]] as ['all' | 'mine' | 'their', string][]).map(([k, l]) =>
            <button key={k} className={'v-chip' + (who === k ? ' on' : '')} onClick={() => setWho(k)}>{l}</button>)}</div>}
          {oursList.length ? <div className="g2">{oursList.map(i =>
            <button className="gcard" key={i.id} data-testid="idea-card" onClick={() => go('idea/' + i.id)}><Pic idea={i}>{badge(i) && <small>{badge(i)}</small>}{i.title}</Pic>
              <small>{CATS[i.category]?.name || 'Идея'} · {i.mine ? 'моя' : 'от ' + ncase(pn, 'gen')}{wants(i.my_wish) ? ' · ♥' : ''}</small></button>)}</div>
            : <Empty title="Здесь пусто"><button className="b p w" onClick={onAdd}>Добавить идею</button></Empty>}
        </>}
    </div>)
}

// ---------- one card for every idea ----------
type IdeaRes = { idea: Idea; filled?: Record<string, 'me' | 'partner' | 'scout'>; enriching?: boolean }

export function IdeaCard({ id, home, from }: { id: number; home: HomeV2 | null; from: string }) {
  const { data, refresh } = useLoad<IdeaRes>('/ideas/' + id, 30000)
  const [edit, setEdit] = useState(false)
  const [busy, setBusy] = useState(false)
  const pn = home?.partner?.name || 'партнёр'
  const i = data?.idea
  if (!i) return <div className="scr"><div className="subh"><button className="back" aria-label="Назад" onClick={() => back('')}>‹ {from}</button><b>Идея</b><span /></div></div>
  const filled = data.filled || {}
  const by = (f: string) => filled[f] === 'me' ? 'я' : filled[f] === 'partner' ? pn : filled[f] === 'scout' ? 'скаут' : ''
  const wait = collecting(i)
  const rows: [string, string, string][] = [
    ['Когда', i.starts_at ? whenText(i.starts_at, i.ends_at) : '', 'when'], ['Где', i.address || '', 'where'],
    ['Цена на двоих', i.price_min ? money(i.price_min) + (i.price_max && i.price_max !== i.price_min ? '–' + money(i.price_max) : '') : '', 'price'],
    ['Программа', i.info?.about || '', 'about'], ['Заметка', i.note || '', 'note'],
  ]
  const miss = rows.filter(([, v, f]) => !v && (f === 'where' || f === 'price')).map(([l]) => l.toLowerCase())
  const meet = home?.next?.chosen?.id === i.id ? home.next : home?.round?.chosen?.id === i.id ? home.round : null
  const mark = async (w: 'want' | 'skip') => {
    try {
      const r = await post<{ idea?: Idea }>(`/ideas/${i.id}/wish`, { wish: i.my_wish === w ? '' : w })
      if (w === 'want' && i.my_wish !== 'want' && wants(r.idea?.partner_wish)) { haptic.ok(); say('Совпало: идея в «Совпало»') }
      refresh(); emit('ideas'); emit('home')
    } catch (e) { say(humanError(e)) }
  }
  const enrich = async () => { try { await post(`/ideas/${i.id}/enrich`); say('Скаут получил запрос'); refresh() } catch (e) { say(humanError(e)) } }
  return (
    <div className="scr" data-testid="v2-idea">
      <div className="subh"><button className="back" aria-label="Назад" onClick={() => back('')}>‹ {from}</button><b>Идея</b><span /></div>
      <Pic idea={i} style={{ minHeight: 150 }}>{i.title}<small>{CATS[i.category]?.name || 'Идея'} · {i.ours ? (i.mine ? 'моя идея' : 'от ' + ncase(pn, 'gen')) : 'афиша'}</small></Pic>
      {wait ? <div className="v-card prop" data-testid="collecting"><b>🔭 Скаут собирает остальное</b><p className="m">Название взято из ссылки сразу. Даты, адрес и цена: обычно до часа. Напишу на главной и в боте.</p></div>
        : data.enriching ? <div className="v-card prop" data-testid="enriching"><b>🔭 Скаут дополняет</b><p className="m">Ищет: {miss.join(', ')}. Напишу на главной и в боте.</p></div>
          : i.mine && miss.length > 0 ? <div className="v-card"><p className="m">Не хватает: {miss.join(', ')}</p><div className="v-row"><button className="b s" onClick={() => setEdit(true)}>Дописать</button><button className="b s" data-testid="ask-scout" onClick={enrich}>Попросить скаута</button></div></div> : null}
      <div className="v-card v-list flds" data-testid="fields">
        {rows.filter(([, v]) => v || i.mine).map(([l, v, f]) =>
          <div className="fld" key={f}><small>{l}{v && by(f) && <span className="by">{by(f)}</span>}</small><span className={v ? '' : 'v-empty'}>{v || (wait && f !== 'note' && f !== 'about' ? 'скаут собирает' : 'не заполнено')}</span></div>)}
      </div>
      {home?.paired && <>
        <div className="blk"><label>Отметка</label><div className="v-row">
          <button className={'v-chip' + (wants(i.my_wish) ? ' on' : '')} aria-pressed={wants(i.my_wish)} onClick={() => mark('want')}>♥ Хочу</button>
          <button className={'v-chip' + (i.my_wish === 'skip' ? ' on' : '')} aria-pressed={i.my_wish === 'skip'} onClick={() => mark('skip')}>Не моё</button>
          <span className="sp" /><span className="m">{wants(i.partner_wish) ? pn + ' хочет' : i.partner_wish === 'skip' ? pn + ': не моё' : i.partner_marked ? pn + ' уже отметил' + (/[ая]$/i.test(pn) ? 'а' : '') : pn + ' ещё не смотрел' + (/[ая]$/i.test(pn) ? 'а' : '')}</span>
        </div></div>
        {!wait && <button className="b a w" data-testid="go-meet" disabled={busy} onClick={async () => { if (meet) { go('meet/' + meet.id); return } setBusy(true); await goMeet(i); setBusy(false) }}>
          {meet ? 'Открыть встречу' : wants(i.partner_wish) || i.partner_marked && wants(i.my_wish) ? 'Идём' : 'Позвать ' + ncase(pn, 'acc')}</button>}
      </>}
      {i.mine && <button className="b w" data-testid="edit-idea" onClick={() => setEdit(true)}>Изменить</button>}
      <Sheet open={edit} onClose={() => setEdit(false)} testid="edit-sheet"><EditIdea idea={i} onDone={() => { setEdit(false); refresh(); emit('ideas') }} /></Sheet>
    </div>)
}

const local = (t?: number) => { if (!t) return ''; const d = new Date(t * 1000 - new Date().getTimezoneOffset() * 60000); return d.toISOString().slice(0, 16) }

function EditIdea({ idea: i, onDone }: { idea: Idea; onDone: () => void }) {
  const [f, setF] = useState({ title: i.title, when: local(i.starts_at), where: i.address || '', price: i.price_min ? String(Math.round(i.price_min / 100)) : '', about: i.info?.about || '', note: i.note || '' })
  const [busy, setBusy] = useState(false)
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value })
  const save = async () => {
    setBusy(true)
    const b: Record<string, unknown> = {}
    if (f.title.trim() !== i.title) b.title = f.title
    if (f.when !== local(i.starts_at)) b.starts_at = f.when ? Math.floor(new Date(f.when).getTime() / 1000) : 0
    if (f.where !== (i.address || '')) b.address = f.where
    const kop = f.price.trim() ? Math.round(Number(f.price.replace(/\s/g, '').replace(',', '.')) * 100) : 0
    if (!Number.isNaN(kop) && kop !== (i.price_min || 0)) b.price_min = kop
    if (f.about !== (i.info?.about || '')) b.about = f.about
    if (f.note !== (i.note || '')) b.note = f.note
    try { if (Object.keys(b).length) await patch('/ideas/' + i.id, b); say('Сохранено'); onDone() } catch (e) { say(humanError(e)) } finally { setBusy(false) }
  }
  return (<>
    <h3>Изменить идею</h3>
    <label className="fld"><small>Название</small><input value={f.title} onChange={set('title')} maxLength={80} /></label>
    <label className="fld"><small>Когда</small><input type="datetime-local" value={f.when} onChange={set('when')} /></label>
    <label className="fld"><small>Где</small><input value={f.where} onChange={set('where')} placeholder="адрес или место" /></label>
    <label className="fld"><small>Цена на двоих, ₽</small><input inputMode="numeric" value={f.price} onChange={set('price')} placeholder="например, 3000" /></label>
    <label className="fld"><small>Программа</small><textarea rows={2} value={f.about} onChange={set('about')} placeholder="что входит, во сколько сбор" /></label>
    <label className="fld"><small>Заметка</small><textarea rows={2} value={f.note} onChange={set('note')} placeholder="для себя и партнёра" /></label>
    <button className="b p w" disabled={busy || !f.title.trim()} data-testid="save-idea" onClick={save}>Сохранить</button>
  </>)
}

// ---------- a new idea: a link or a name; the card is there at once, the scout fills the rest ----------
export function AddIdea({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const add = async () => {
    setBusy(true)
    try {
      const r = await post<{ idea?: Idea; existing?: Idea }>('/ideas', { text })
      const i = r.idea || r.existing
      setText(''); onClose(); emit('ideas'); emit('home')
      say(r.existing ? 'Такая идея уже есть' : 'Идея добавлена')
      if (i) go('idea/' + i.id)
    } catch (e) { say(humanError(e)) } finally { setBusy(false) }
  }
  return (
    <Sheet open={open} onClose={onClose} testid="add-sheet">
      <h3>Новая идея</h3>
      <label className="fld"><small>Ссылка или название</small><input autoFocus value={text} onChange={e => setText(e.target.value)} placeholder="afisha.yandex.ru/… или «Каток на ВДНХ»" onKeyDown={e => { if (e.key === 'Enter' && text.trim()) add() }} /></label>
      <p className="m">Карточка появится сразу. По ссылке скаут сам соберёт даты, адрес и цену, потом сможешь поправить любое поле.</p>
      <button className="b p w" disabled={busy || !text.trim()} data-testid="add-save" onClick={add}>Добавить</button>
    </Sheet>)
}
