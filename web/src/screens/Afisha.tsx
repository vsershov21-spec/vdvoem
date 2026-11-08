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
import { Cover, Empty, Segmented, Skel, SubHeader, toast } from '../ui/kit'

type KV = { k: string; n: number }
type Res = { ideas: Idea[]; counts: Record<string, number>; genres: KV[]; cats: KV[] }
type F = { hz: 'all' | 'week' | 'later'; source: 'all' | 'scout' | 'channel' | 'ours'; sort: 'match' | 'date' | 'price'; fresh: boolean; genre: string; cat: string }
const SRC: [F['source'], string][] = [['all', 'Все'], ['scout', '🔭 Скаут'], ['channel', '📣 Каналы'], ['ours', '🗂 Наши']]
const SRC_BADGE: Record<string, string> = { scout: '🔭 скаут', channel: '📣 канал' }
const load = (): F => { try { return { hz: 'all', source: 'all', sort: 'match', fresh: false, genre: '', cat: '', ...JSON.parse(localStorage.getItem('vd-afisha') || '{}') } } catch { return { hz: 'all', source: 'all', sort: 'match', fresh: false, genre: '', cat: '' } } }

const when = (i: Idea) => {
  if (!i.starts_at) return ''
  const d = new Date(i.starts_at * 1000), L = dayLabel(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`)
  const t = d.getHours() || d.getMinutes() ? `, ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}` : ''
  return `${L.wd} ${L.num} ${L.mon}${t}`
}

export function Afisha() {
  const [f, setF] = useState<F>(load)
  const upd = (p: Partial<F>) => { haptic.select(); setF(x => { const n = { ...x, ...p }; try { localStorage.setItem('vd-afisha', JSON.stringify(n)) } catch { /* private */ } return n }) }
  const qs = new URLSearchParams({ horizon: f.hz, source: f.source, sort: f.sort, ...(f.fresh ? { fresh: '1' } : {}), ...(f.genre ? { genre: f.genre } : {}), ...(f.cat ? { cat: f.cat } : {}) })
  const res = useLoad<Res>('/afisha?' + qs)
  const mark = async (i: Idea, w: Wish) => {
    haptic.tap()
    try {
      await post('/ideas/' + i.id + '/wish', { wish: w })
      res.set(r => r && { ...r, ideas: r.ideas.map(x => x.id === i.id ? { ...x, my_wish: w } : x) })
      emit('ideas'); emit('home')
      if (w !== 'skip') toast(w === 'very' ? 'Очень хочу ✓ — в копилке' : 'Хочу ✓ — в копилке')
    } catch (e) { toast(humanError(e)) }
  }
  const counts = res.data?.counts || {}
  const n = (k: string) => counts[k] != null ? <b>{counts[k]}</b> : null
  return (<>
    <SubHeader title="Афиша" />
    <div className="wkchips fchips" role="tablist" aria-label="Когда">
      {([['all', 'Всё'], ['week', 'Ближайшие 2 недели'], ['later', 'Анонсы']] as [F['hz'], string][]).map(([k, l]) =>
        <button key={k} className="wkchip" role="tab" aria-selected={f.hz === k} data-on={f.hz === k || undefined} onClick={() => upd({ hz: k })}>{l}{n(k)}</button>)}
    </div>
    <div className="wkchips fchips" aria-label="Источник">
      {SRC.map(([k, l]) => <button key={k} className="wkchip" data-on={f.source === k || undefined} aria-pressed={f.source === k} onClick={() => upd({ source: k })}>{l}{k !== 'all' ? n(k) : null}</button>)}
      <button className="wkchip" data-on={f.fresh || undefined} aria-pressed={f.fresh} onClick={() => upd({ fresh: !f.fresh })}>🆕 Ещё не видели{n('fresh')}</button>
    </div>
    <div className="afisha__sort">
      <Segmented<F['sort']> items={[['match', 'По интересам'], ['date', 'По дате'], ['price', 'Дешевле']]} value={f.sort} onChange={v => upd({ sort: v })} />
    </div>
    {!!res.data?.genres?.length && <div className="wkchips fchips" aria-label="Жанр">
      {f.genre && <button className="wkchip" onClick={() => upd({ genre: '' })}><Icon n="x" s={12} />Все жанры</button>}
      {res.data.genres.map(g => <button key={g.k} className="wkchip" data-on={f.genre === g.k || undefined} onClick={() => upd({ genre: f.genre === g.k ? '' : g.k })}>{g.k}<b>{g.n}</b></button>)}
    </div>}
    <div className="row" style={{ margin: '2px 2px 10px', gap: 8 }}>
      <span className="caption muted" style={{ flex: 1 }}>{res.data ? `${res.data.ideas.length} ${res.data.ideas.length % 10 === 1 && res.data.ideas.length % 100 !== 11 ? 'событие' : 'событий'}` : ' '}{f.sort === 'match' ? ' · сверху то, что понравится вам обоим' : ''}</span>
      <button className="link" onClick={() => go('assistant')}><Icon n="spark" s={14} />Спросить помощника</button>
    </div>
    {!res.data ? <div className="col">{[0, 1, 2].map(k => <Skel key={k} h={150} />)}</div>
      : res.data.ideas.length === 0 ? <Empty title="Под эти фильтры пусто" text={f.genre || f.fresh || f.source !== 'all' || f.hz !== 'all'
        ? 'Сними фильтр — или скажи помощнику «следи за …», и скаут поищет специально.'
        : 'Скаут приносит события под ваши вкусы, каналы — посты, которые ты пересылаешь боту.'} />
      : <div className="col">{res.data.ideas.map((i, k) => (
        <motion.article key={i.id} className="acard" initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: Math.min(k, 6) * 0.04, type: 'spring', stiffness: 400, damping: 30 }}>
          <button className="acard__main" onClick={() => go('idea/' + i.id)}>
            <Cover idea={i} h={132}>
              {i.match && <span className="acard__match" title={i.match.note}>{i.match.both}%</span>}
              <span className="acard__src">{SRC_BADGE[i.source || ''] || '🗂 наше'}</span>
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
