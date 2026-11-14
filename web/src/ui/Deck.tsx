import type React from 'react'
import { useEffect, useState } from 'react'
import { motion, useMotionValue, useTransform, AnimatePresence } from 'framer-motion'
import type { Idea, Person, Wish } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, humanError } from '../lib/api'
import { go } from '../lib/router'
import { haptic } from '../lib/tg'
import { past } from '../lib/format'
import { startWith } from '../lib/startWith'
import { Icon } from './icons'
import { Btn, Cover, Empty, Sheet, Skel, burst, toast } from './kit'

// The deck — «Вдвоём» à la Tinder: every idea and event without your mark, one card at a time. Each swipes on
// their own; when both want the same thing it is a match, and the next step is picking the time.
// right — хочу, up — очень, left — пропущу; the round buttons below do the same.
// ids marked in this session: a deck reopened from the cache must not show them again
const marked = new Set<number>()

export function Deck({ partner, onEmpty }: { partner?: Person; onEmpty?: React.ReactNode }) {
  const { data, refresh } = useLoad<{ ideas: Idea[]; total: number }>('/triage')
  const [k, setK] = useState(0)
  const [done, setDone] = useState(0)
  const [busy, setBusy] = useState(false)
  const [match, setMatch] = useState<Idea | null>(null)
  // the deck is fixed when it opens: marks made here (and the partner's live updates) must not reshuffle it
  const [list, setList] = useState<Idea[] | null>(null)
  useEffect(() => { if (data && !list) setList(data.ideas.filter(x => !marked.has(x.id))) }, [data])
  if (!list) return <div className="sdeck"><Skel h={430} r={32} mb={0} /></div>
  const i = list[k], nx = list[k + 1]
  const pn = partner?.name || 'Партнёр'
  const next = () => { setK(x => x + 1); setBusy(false) }
  const wish = async (w: Exclude<Wish, ''>) => {
    if (busy || !i) return
    setBusy(true)
    try {
      const r = await post<{ idea: Idea }>(`/ideas/${i.id}/wish`, { wish: w })
      marked.add(i.id); haptic.select(); setDone(d => d + 1); next()
      const pw = r.idea?.partner_wish
      if (w !== 'skip' && (pw === 'want' || pw === 'very')) { haptic.ok(); burst(); setMatch(r.idea) }
    } catch (e) { toast(humanError(e)); setBusy(false) }
  }
  const later = () => { haptic.select(); next() }
  const urgent = async () => {
    if (busy || !i) return
    setBusy(true)
    try { await post(`/ideas/${i.id}/urgent`, {}); toast(`${pn} получит просьбу ответить`); next() } catch (e) { toast(humanError(e)); setBusy(false) }
  }
  const restart = () => { setList(null); setK(0); refresh(); emit('home') }
  const sheet = <MatchSheet idea={match} partner={pn} onClose={() => setMatch(null)} />
  if (!i) return (<>
    <div className="sdeck sdeck--empty card rise">
      <Empty title={list.length ? 'Всё разобрали' : 'Свайпать пока нечего'} text={done ? `Отмечено: ${done}. Совпадения — в «Идеи → Хотите оба». Скаут принесёт новое утром.` : 'Все идеи и события уже с отметкой. Добавь идею или спроси помощника.'}>
        <div className="row" style={{ gap: 8, justifyContent: 'center', flexWrap: 'wrap' }}>
          <Btn kind="soft" sm icon="heart" onClick={() => go('ideas/both')}>Хотите оба</Btn>
          <Btn kind="ghost" sm icon="history" onClick={restart}>Обновить</Btn>
        </div>
      </Empty>
      {onEmpty}
    </div>
    {sheet}
  </>)
  const when = i.starts_at ? new Date(i.starts_at * 1000).toLocaleString('ru', { weekday: 'short', day: 'numeric', month: 'long', ...(new Date(i.starts_at * 1000).getHours() ? { hour: '2-digit', minute: '2-digit' } : {}) }) : ''
  return (<>
    <div className="sdeck" data-testid="deck">
      {nx && <div className="sdeck__peek" aria-hidden="true"><Cover idea={nx} h={230} big><span /><span /></Cover></div>}
      <AnimatePresence initial={false}>
        <Swipe key={i.id} onSwipe={d => wish(d === 'right' ? 'want' : d === 'up' ? 'very' : 'skip')}>
          <div className="sdeck__card" data-testid="triage-card">
            <Cover idea={i} h={230} big>
              <span className="row" style={{ gap: 6, flexWrap: 'wrap' }}>
                {when && <span className="pill pill--glass"><Icon n="cal" s={12} w={2.2} />{when}</span>}
                {i.partner_marked && <span className="pill pill--glass">{pn} {past(partner?.name, 'отметил')}</span>}
              </span>
              <div className="cover__title" style={{ fontSize: 24, lineHeight: '28px' }}>{i.title}</div>
            </Cover>
            <div className="sdeck__body">
              {i.meta && <p className="callout" style={{ margin: 0, fontWeight: 600 }}>{i.meta}</p>}
              {i.why ? <p className="caption muted sdeck__why">{i.why}</p> : i.info?.about ? <p className="caption muted sdeck__why">{i.info.about}</p> : i.match?.note ? <p className="caption muted sdeck__why">{i.match.note}</p> : null}
            </div>
          </div>
        </Swipe>
      </AnimatePresence>
    </div>
    <div className="sdeck__acts">
      <button className="sdeck__btn" aria-label="Пропущу" onClick={() => wish('skip')} disabled={busy}><Icon n="x" s={26} w={2.4} /></button>
      <button className="sdeck__btn sdeck__btn--sm" aria-label="Очень хочу" onClick={() => wish('very')} disabled={busy}><Icon n="flame" s={22} w={2.2} /></button>
      <button className="sdeck__btn sdeck__btn--yes" aria-label="Хочу" onClick={() => wish('want')} disabled={busy}><Icon n="heart" s={26} w={2.4} /></button>
    </div>
    <div className="sdeck__more">
      <span className="caption muted">{k + 1} из {list.length}</span>
      <button className="link" onClick={later}>Потом</button>
      {partner && <button className="link" onClick={urgent}>Важно — пусть {pn} ответит</button>}
    </div>
    {sheet}
  </>)
}

// «Совпало!»: both want it — the next step is the time (a round with this idea, starting at the time step)
function MatchSheet({ idea, partner, onClose }: { idea: Idea | null; partner: string; onClose: () => void }) {
  return (
    <Sheet open={!!idea} onClose={onClose}>
      {idea && <div className="col" style={{ alignItems: 'center', textAlign: 'center', gap: 12, padding: '4px 0 6px' }} data-testid="match-sheet">
        <div className="matchmark" aria-hidden="true"><i /><i /></div>
        <h2 className="display" style={{ margin: 0 }}>Совпало!</h2>
        <p className="callout muted" style={{ margin: 0 }}>{partner} тоже хочет: <b style={{ color: 'var(--ink)' }}>{idea.title}</b></p>
        <div className="col" style={{ gap: 8, width: '100%', marginTop: 6 }}>
          <Btn kind="primary" block icon="cal" onClick={async () => { if (await startWith(idea.id, idea.title)) onClose() }}>Выбрать время</Btn>
          <Btn kind="soft" block onClick={onClose}>Свайпать дальше</Btn>
        </div>
      </div>}
    </Sheet>
  )
}

// a card you can throw: right — хочу, up — очень, left — пропущу
function Swipe({ children, onSwipe }: { children: React.ReactNode; onSwipe: (d: 'left' | 'right' | 'up') => void }) {
  const x = useMotionValue(0), y = useMotionValue(0)
  const rotate = useTransform(x, [-200, 200], [-12, 12])
  const like = useTransform(x, [30, 120], [0, 1]), nope = useTransform(x, [-120, -30], [1, 0]), very = useTransform(y, [-120, -30], [1, 0])
  const [out, setOut] = useState<'' | 'left' | 'right' | 'up'>('')
  return (
    <motion.div className="swipe" style={{ x, y, rotate, touchAction: 'none' }} drag dragSnapToOrigin={!out} dragElastic={0.9}
      initial={{ opacity: 0, scale: 0.94, y: 12 }} animate={{ opacity: 1, scale: 1, y: 0 }}
      exit={{ x: out === 'left' ? -460 : out === 'right' ? 460 : 0, y: out === 'up' ? -700 : 0, opacity: 0, transition: { duration: 0.28 } }}
      onDragEnd={(_, info) => {
        const { offset: o, velocity: v } = info
        const d = o.y < -110 || v.y < -700 ? 'up' : o.x > 110 || v.x > 700 ? 'right' : o.x < -110 || v.x < -700 ? 'left' : ''
        if (d) { setOut(d); onSwipe(d) }
      }}>
      <motion.span className="swipe__tag swipe__tag--like" style={{ opacity: like }}>Хочу</motion.span>
      <motion.span className="swipe__tag swipe__tag--nope" style={{ opacity: nope }}>Пропущу</motion.span>
      <motion.span className="swipe__tag swipe__tag--very" style={{ opacity: very }}>Очень!</motion.span>
      {children}
    </motion.div>
  )
}
