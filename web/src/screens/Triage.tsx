import { useState } from 'react'
import type { Idea, Person } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, del, humanError } from '../lib/api'
import { back, go } from '../lib/router'
import { haptic } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Btn, Cover, Empty, Skel, SubHeader, toast } from '../ui/kit'

// «Разобрать всё»: every unmarked idea and event, one card at a time — soonest dates first, so nothing
// that is about to sell out waits at the bottom of a long list.
export function Triage({ partner }: { partner?: Person }) {
  const { data } = useLoad<{ ideas: Idea[]; total: number }>('/triage')
  const [k, setK] = useState(0)
  const [done, setDone] = useState(0)
  const [busy, setBusy] = useState(false)
  if (!data) return <><SubHeader title="Разбор" /><Skel h={360} r={26} /><Skel h={60} /></>
  const list = data.ideas
  const i = list[k]
  const next = () => { setK(x => x + 1); setBusy(false) }
  const act = async (f: () => Promise<unknown>, ok?: string) => {
    if (busy || !i) return
    setBusy(true)
    try { await f(); haptic.select(); setDone(d => d + 1); if (ok) toast(ok); next() } catch (e) { toast(humanError(e)); setBusy(false) }
  }
  const wish = (w: 'very' | 'want' | 'skip') => act(() => post(`/ideas/${i.id}/wish`, { wish: w }))
  const urgent = () => act(() => post(`/ideas/${i.id}/urgent`, {}), `${partner?.name || 'Партнёр'} получит просьбу ответить`)
  const archive = () => act(() => del('/ideas/' + i.id))
  if (!i) return (<>
    <SubHeader title="Разбор" />
    <div className="card rise"><Empty title={list.length ? 'Всё разобрали' : 'Разбирать нечего'} text={done ? `Отмечено: ${done}. Совпадения — в «Хотите оба», скоро помощник предложит лучшее.` : 'Все идеи и события уже с отметкой.'}>
      <Btn kind="primary" sm onClick={() => { emit('home'); emit('ideas'); back('') }}>Готово</Btn>
      {done > 0 && <Btn kind="soft" sm onClick={() => go('ideas/both')}>Хотите оба</Btn>}
    </Empty></div>
  </>)
  const when = i.starts_at ? new Date(i.starts_at * 1000).toLocaleString('ru', { weekday: 'short', day: 'numeric', month: 'long', ...(new Date(i.starts_at * 1000).getHours() ? { hour: '2-digit', minute: '2-digit' } : {}) }) : ''
  return (<>
    <SubHeader title={`Разбор · ${k + 1} из ${list.length}`} />
    <div className="progress rise" style={{ ['--p' as any]: `${Math.round(100 * k / list.length)}%` }}><i /></div>
    <div key={i.id} className="card rise" data-testid="triage-card" style={{ padding: 8 }}>
      <Cover idea={i} h={220} big>
        <span>{when && <span className="pill pill--glass"><Icon n="cal" s={12} w={2.2} />{when}</span>}</span>
        <div className="cover__title" style={{ fontSize: 22, lineHeight: '26px' }}>{i.title}</div>
      </Cover>
      <div className="col" style={{ gap: 8, padding: '12px 8px 6px' }}>
        {i.meta && <p className="caption muted" style={{ margin: 0 }}>{i.meta}</p>}
        {!!i.info?.genres?.length && <div className="row" style={{ flexWrap: 'wrap', gap: 6 }}>{i.info.genres.map(g => <span key={g} className="pill">{g}</span>)}</div>}
        {i.info?.about && <p className="callout" style={{ margin: 0 }}>{i.info.about}</p>}
        {i.why && <p className="acard__why" style={{ margin: 0 }}>{i.why}</p>}
        {i.match?.note && <p className="caption acard__note" style={{ margin: 0 }}>Совпадение {i.match.both}% · {i.match.note}</p>}
        {i.partner_marked && <p className="caption muted" style={{ margin: 0 }}>{partner?.name || 'Партнёр'} уже отметил(а) — увидишь после своей отметки</p>}
      </div>
    </div>
    <div className="triage__acts rise" style={{ ['--d' as any]: 1 }}>
      <Btn kind="accent" block shine icon="heart" onClick={() => wish('very')}>Очень хочу</Btn>
      <div className="row" style={{ gap: 8 }}>
        <Btn kind="surface" block onClick={() => wish('want')}>Хочу</Btn>
        <Btn kind="surface" block onClick={() => wish('skip')}>Пропущу</Btn>
      </div>
      <div className="row" style={{ gap: 8 }}>
        {partner && <Btn kind="soft" block icon="flame" onClick={urgent}>Важно</Btn>}
        <Btn kind="ghost" block icon="trash" onClick={archive}>Убрать</Btn>
        <Btn kind="ghost" block onClick={() => { haptic.select(); next() }}>Потом</Btn>
      </div>
    </div>
  </>)
}
