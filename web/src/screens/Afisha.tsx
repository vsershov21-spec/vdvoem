// «Афиша» — what the scout found: the week ahead and announcements 1–3 months out, ranked by how much
// you both would like it (each person's own learned taste). Images load straight from the listing sites.
import { useState } from 'react'
import { motion } from 'framer-motion'
import type { Idea, Wish } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, humanError } from '../lib/api'
import { go } from '../lib/router'
import { dayLabel, money } from '../lib/format'
import { haptic } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Cover, Empty, Skel, SubHeader, toast } from '../ui/kit'

type Res = { ideas: Idea[]; counts: { week: number; later: number } }

const when = (i: Idea) => {
  if (!i.starts_at) return ''
  const d = new Date(i.starts_at * 1000), L = dayLabel(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`)
  const t = d.getHours() || d.getMinutes() ? `, ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}` : ''
  return `${L.wd} ${L.num} ${L.mon}${t}`
}

export function Afisha() {
  const [hz, setHz] = useState<'week' | 'later'>('week')
  const res = useLoad<Res>('/afisha?horizon=' + hz)
  const mark = async (i: Idea, w: Wish) => {
    haptic.tap()
    try {
      await post('/ideas/' + i.id + '/wish', { wish: w })
      res.set(r => r && { ...r, ideas: r.ideas.map(x => x.id === i.id ? { ...x, my_wish: w } : x) })
      emit('ideas'); emit('home')
      if (w !== 'skip') toast(w === 'very' ? 'Очень хочу ✓ — в копилке' : 'Хочу ✓ — в копилке')
    } catch (e) { toast(humanError(e)) }
  }
  const counts = res.data?.counts
  return (<>
    <SubHeader title="Афиша скаута" />
    <div className="wkchips fchips" role="tablist">
      <button className="wkchip" role="tab" aria-selected={hz === 'week'} data-on={hz === 'week' || undefined} onClick={() => setHz('week')}>На неделю{counts ? <b>{counts.week}</b> : null}</button>
      <button className="wkchip" role="tab" aria-selected={hz === 'later'} data-on={hz === 'later' || undefined} onClick={() => setHz('later')}>Анонсы{counts ? <b>{counts.later}</b> : null}</button>
      <button className="wkchip" onClick={() => go('assistant')}><Icon n="spark" s={14} />Уточнить у помощника</button>
    </div>
    {!res.data ? <div className="col">{[0, 1, 2].map(k => <Skel key={k} h={150} />)}</div>
      : res.data.ideas.length === 0 ? <Empty title={hz === 'week' ? 'Пока пусто' : 'Анонсов нет'} text={hz === 'week'
        ? 'Скаут приходит раз в неделю и приносит 15–20 событий под ваши вкусы. Пока его не было — или всё уже разобрали.'
        : 'Здесь будут премьеры и концерты на 1–3 месяца вперёд. Скажи помощнику «следи за …» — скаут поищет специально.'} />
      : <div className="col">{res.data.ideas.map((i, k) => (
        <motion.article key={i.id} className="acard" initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: Math.min(k, 6) * 0.04, type: 'spring', stiffness: 400, damping: 30 }}>
          <button className="acard__main" onClick={() => go('idea/' + i.id)}>
            <Cover idea={i} h={132}>
              {i.match && <span className="acard__match" title={i.match.note}>{i.match.both}%</span>}
            </Cover>
            <span className="acard__body">
              <b className="acard__title">{i.title}</b>
              <span className="caption muted">{[when(i), i.price_min ? '≈ ' + money(i.price_min) : '', i.address].filter(Boolean).join(' · ')}</span>
              {i.why && <span className="acard__why">{i.why}</span>}
              {i.match?.note && <span className="caption acard__note">{i.match.note}</span>}
              {i.on_sale && i.on_sale * 1000 > Date.now() && <span className="caption acard__note">Продажа билетов с {when({ ...i, starts_at: i.on_sale })}</span>}
            </span>
          </button>
          <div className="acard__acts">
            {(['want', 'very', 'skip'] as Wish[]).map(w => (
              <button key={w} className="wkchip" data-on={i.my_wish === w || undefined} aria-pressed={i.my_wish === w} onClick={() => mark(i, w)}>
                {w === 'very' && <Icon n="heart" s={13} w={2.4} />}{w === 'want' ? 'Хочу' : w === 'very' ? 'Очень' : 'Пропущу'}</button>
            ))}
          </div>
        </motion.article>
      ))}</div>}
  </>)
}
