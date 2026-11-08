import { useState } from 'react'
import { startWith } from '../lib/startWith'
import type { Idea, Person, Wish } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, patch, del, humanError } from '../lib/api'
import { back } from '../lib/router'
import { CATS, money, dur, isFixed } from '../lib/format'
import { openLink } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Btn, Cover, Sheet, Skel, SubHeader, WishPicker, toast, Segmented } from '../ui/kit'
import { Places } from '../ui/Places'

const wishWord: Record<string, string> = { want: 'хочет', very: 'очень хочет', skip: 'пропустит' }

export function IdeaScreen({ id, me, partner: pp }: { id: number; me?: Person; partner?: Person }) {
  const { data, set } = useLoad<{ idea: Idea }>('/ideas/' + id, 30000)
  const [edit, setEdit] = useState(false)
  if (!data || !me) return <><SubHeader /><Skel h={280} r={26} /><Skel h={60} /><Skel h={120} /></>
  const i = data.idea
  const wish = async (w: Wish) => {
    try { const r = await post<{ idea: Idea }>(`/ideas/${i.id}/wish`, { wish: w || 'skip' }); set(r); emit('home') } catch (e) { toast(humanError(e)) }
  }
  const goWith = async () => {
    await startWith(i.id, i.title)
  }
  const archive = async () => { await del('/ideas/' + i.id); emit('ideas'); emit('home'); toast('Убрал в архив'); back('ideas') }
  const restore = async () => { const r = await patch<{ idea: Idea }>('/ideas/' + i.id, { status: 'active' }); set(r); emit('ideas'); toast('Вернул в копилку') }
  const fixed = isFixed(i)
  const partner = i.partner_wish ? wishWord[i.partner_wish] : i.partner_marked ? 'отметил(а) — увидишь после своей отметки' : 'ещё не отметил(а)'
  return (
    <>
      <SubHeader right={<button className="iconbtn" onClick={() => setEdit(true)} aria-label="Изменить"><Icon n="edit" s={20} /></button>} />
      <div className="rise"><Cover idea={i} h={280} big>
        <div className="row"><span className="pill pill--glass">{CATS[i.category]?.name}</span>{i.kind === 'chore' && <span className="pill pill--glass">дело</span>}</div>
        <h1 className="cover__title" style={{ fontSize: 26, lineHeight: '30px', margin: 0 }}>{i.title}</h1>
      </Cover></div>
      <div className="card rise" style={{ ['--d' as any]: 1, marginTop: 12, display: 'grid', gap: 14 }}>
        {i.address && <div className="trow"><Icon n="pin" s={20} /><span className="callout">{i.address}</span></div>}
        {!!i.price_min && <div className="trow"><Icon n="wallet" s={20} /><span className="callout">≈ {money(i.price_min)} на двоих</span></div>}
        {!!i.duration_min && <div className="trow"><Icon n="clock" s={20} /><span className="callout">{dur(i.duration_min)}</span></div>}
        {fixed && <div className="trow"><Icon n="clock" s={20} /><span className="callout" data-testid="fixed-when">{new Date(i.starts_at! * 1000).toLocaleString('ru', { weekday: 'short', day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' })}</span></div>}
        {i.ends_at && !fixed && <div className="trow"><Icon n="cal" s={20} /><span className="callout">До {new Date(i.ends_at * 1000).toLocaleDateString('ru', { day: 'numeric', month: 'long' })}</span></div>}
        {i.why && <p className="acard__why" style={{ margin: 0 }}>{i.why}</p>}
        {i.match && i.match.note && <p className="caption acard__note" style={{ margin: 0 }} data-testid="match">Совпадение {i.match.both}% · {i.match.note}</p>}
        {i.note && <p className="callout muted" style={{ margin: 0 }}>{i.note}</p>}
        {i.url && <Btn kind="soft" sm icon="link" onClick={() => openLink(i.url!)}>Открыть ссылку</Btn>}
      </div>
      {i.kind === 'leisure' && <div className="rise" style={{ ['--d' as any]: 2, marginTop: 12 }}><Places idea={i} me={me} partner={pp} onChange={n => set({ idea: n })} /></div>}
      {i.status === 'active' && i.kind === 'leisure' && <>
        <div className="sec"><h2 className="headline">Твоя оценка</h2></div>
        <WishPicker value={i.my_wish} onChange={wish} />
        <p className="caption muted" style={{ margin: '10px 4px 0' }}>Партнёр {partner}.</p>
        <div style={{ marginTop: 20 }}><Btn kind="accent" block shine icon="cal" onClick={goWith}>{fixed ? 'Идём! Позвать и в календарь' : 'Идём сюда — найти время'}</Btn></div>
      </>}
      {i.status === 'draft' && i.kind === 'leisure' && fixed && <div style={{ marginTop: 20 }}><Btn kind="accent" block shine icon="cal" onClick={goWith}>Идём! Позвать и в календарь</Btn></div>}
      <div style={{ marginTop: 10 }}>
        {i.status === 'active' ? <Btn kind="ghost" block icon="trash" onClick={archive}>Убрать в архив</Btn> : <Btn kind="soft" block onClick={restore}>Вернуть в копилку</Btn>}
      </div>
      <EditSheet idea={i} open={edit} onClose={() => setEdit(false)} onSaved={r => { set({ idea: r }); emit('ideas'); emit('home') }} me={me} />
    </>
  )
}

function EditSheet({ idea, open, onClose, onSaved, me }: { idea: Idea; open: boolean; onClose: () => void; onSaved: (i: Idea) => void; me: Person }) {
  const [f, setF] = useState({ title: idea.title, address: idea.address || '', note: idea.note || '', price: idea.price_min ? String(Math.round(idea.price_min / 100)) : '',
    dur: idea.duration_min ? String(idea.duration_min) : '', category: idea.category, kind: idea.kind, assignee: idea.assignee || '' })
  const up = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value })
  const save = async () => {
    try {
      const p = Number(f.price.replace(/\D/g, '')) * 100
      const r = await patch<{ idea: Idea }>('/ideas/' + idea.id, { title: f.title, address: f.address, note: f.note, price_min: p, price_max: p,
        duration_min: Number(f.dur) || 0, category: f.category, kind: f.kind, assignee: f.assignee })
      onSaved(r.idea); onClose(); toast('Сохранил')
    } catch (e) { toast(humanError(e)) }
  }
  const other = me.color === 'a' ? 'b' : 'a'
  return (
    <Sheet open={open} onClose={onClose}>
      <div className="col">
        <h2 className="title">Исправить</h2>
        <Segmented<'leisure' | 'chore'> items={[['leisure', 'Досуг'], ['chore', 'Дело']]} value={f.kind} onChange={v => setF({ ...f, kind: v })} />
        <div className="field"><label>Название</label><input className="input" value={f.title} maxLength={80} onChange={up('title')} /></div>
        {f.kind === 'leisure' ? <>
          <div className="sugg">{Object.entries(CATS).filter(([k]) => k !== 'home').map(([k, c]) => (
            <button key={k} onClick={() => setF({ ...f, category: k })} style={f.category === k ? { background: 'var(--solid)', color: 'var(--on-solid)', borderColor: 'var(--solid)' } : undefined}>{c.name}</button>))}</div>
          <div className="field"><label>Адрес</label><input className="input" value={f.address} onChange={up('address')} /></div>
          <div className="row" style={{ gap: 10 }}>
            <div className="field" style={{ flex: 1 }}><label>На двоих, ₽</label><input className="input" inputMode="numeric" value={f.price} onChange={up('price')} /></div>
            <div className="field" style={{ flex: 1 }}><label>Длится, мин</label><input className="input" inputMode="numeric" value={f.dur} onChange={up('dur')} /></div>
          </div>
        </> : (
          <div className="field"><label>Кто делает</label>
            <Segmented<string> items={[['', 'Кто угодно'], [me.color, 'Я'], [other, 'Партнёр']]} value={f.assignee} onChange={v => setF({ ...f, assignee: v })} /></div>
        )}
        <div className="field"><label>Заметка</label><textarea className="input" rows={2} value={f.note} onChange={up('note')} /></div>
        <Btn kind="primary" block onClick={save} disabled={!f.title.trim()}>Сохранить</Btn>
      </div>
    </Sheet>
  )
}
