import { motion } from 'framer-motion'
import type { Idea, Wish } from '../lib/types'
import { go } from '../lib/router'
import { dayLabel, money } from '../lib/format'
import { Icon } from './icons'
import { Cover } from './kit'

export const SRC_BADGE: Record<string, string> = { scout: '🔭 скаут', channel: '📣 канал' }

export const when = (i: { starts_at?: number }) => {
  if (!i.starts_at) return ''
  const d = new Date(i.starts_at * 1000), L = dayLabel(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`)
  const t = d.getHours() || d.getMinutes() ? `, ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}` : ''
  return `${L.wd} ${L.num} ${L.mon}${t}`
}

// One event or idea, the same everywhere (afisha, a «подборка» from the assistant): picture, when, price,
// where it came from, why it suits you — and the three marks under the thumb.
export function EventCard({ i, k = 0, onMark, partnerName }: { i: Idea; k?: number; onMark: (i: Idea, w: Wish) => void; partnerName?: string }) {
  const until = !i.starts_at && i.ends_at ? 'до ' + new Date(i.ends_at * 1000).toLocaleDateString('ru', { day: 'numeric', month: 'long' }) : ''
  return (
    <motion.article className="acard" data-testid="event-card" initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: Math.min(k, 6) * 0.04, type: 'spring', stiffness: 400, damping: 30 }}>
      <button className="acard__main" onClick={() => go('idea/' + i.id)}>
        <Cover idea={i} h={132}>
          {i.match && <span className="acard__match" title={i.match.note}>{i.match.both}%</span>}
          <span className="acard__src">{SRC_BADGE[i.source || ''] || '🗂 ваше'}</span>
        </Cover>
        <span className="acard__body">
          <b className="acard__title">{i.title}</b>
          <span className="caption muted">{[when(i) || until, i.price_min ? '≈ ' + money(i.price_min) + ' на двоих' : '', i.address].filter(Boolean).join(' · ')}</span>
          {!!i.info?.genres?.length && <span className="acard__tags">{i.info.genres.slice(0, 3).map(g => <i key={g}>{g}</i>)}</span>}
          {i.info?.about ? <span className="acard__why">{i.info.about}</span> : i.why && <span className="acard__why">{i.why}</span>}
          {(i.partner_wish === 'want' || i.partner_wish === 'very') && <span className="caption acard__note">♥ {partnerName || 'Партнёр'} хочет</span>}
          {i.match?.note && <span className="caption acard__note">{i.match.note}</span>}
          {i.on_sale && i.on_sale * 1000 > Date.now() && <span className="caption acard__note">Продажа билетов с {when({ starts_at: i.on_sale })}</span>}
        </span>
      </button>
      <div className="acard__acts">
        {(['want', 'very', 'skip'] as Wish[]).map(w => (
          <button key={w} className="wkchip" data-on={i.my_wish === w || undefined} aria-pressed={i.my_wish === w} onClick={() => onMark(i, w)}>
            {w === 'very' && <Icon n="heart" s={13} w={2.4} />}{w === 'want' ? 'Хочу' : w === 'very' ? 'Очень' : 'Пропущу'}</button>
        ))}
      </div>
    </motion.article>
  )
}
