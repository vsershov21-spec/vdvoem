// «Где именно»: concrete places for an idea — links parsed by the server, names, or the assistant's picks.
// Both partners add options before or after the match; «Берём этот» sets the place of the meeting.
import { useEffect, useRef, useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import type { Idea, Person } from '../lib/types'
import { get, post, del, humanError, ApiError } from '../lib/api'
import { openLink, haptic } from '../lib/tg'
import { money } from '../lib/format'
import { Icon } from './icons'
import { Btn, toast } from './kit'

export function Places({ idea, me, partner, ai, onChange, compact }: { idea: Idea; me: Person; partner?: Person; ai?: boolean; onChange?: (i: Idea) => void; compact?: boolean }) {
  const [i, setI] = useState(idea)
  const [text, setText] = useState('')
  const [busy, setBusy] = useState<'' | 'add' | 'ai'>('')
  const poll = useRef(0)
  useEffect(() => setI(idea), [idea])
  const apply = (n: Idea) => { setI(n); onChange?.(n) }
  // links are parsed in the background: refresh until every option is parsed
  useEffect(() => {
    clearTimeout(poll.current)
    if (i.options?.some(o => !o.parsed)) poll.current = window.setTimeout(async () => { try { apply((await get<{ idea: Idea }>(`/ideas/${i.id}/options`)).idea) } catch { /* next time */ } }, 1500)
    return () => clearTimeout(poll.current)
  }, [i])
  const add = async () => {
    if (!text.trim()) return
    setBusy('add')
    try { apply((await post<{ idea: Idea }>(`/ideas/${i.id}/options`, { text })).idea); setText(''); haptic.ok() } catch (e) { toast(humanError(e)) } finally { setBusy('') }
  }
  const suggest = async () => {
    setBusy('ai')
    try { apply((await post<{ idea: Idea }>(`/ideas/${i.id}/options/suggest`)).idea); haptic.ok() }
    catch (e) { toast(e instanceof ApiError && e.code === 'no_ai' ? 'Помощник без AI — пришли ссылку или найди на карте' : 'Не нашёл вариантов — пришли ссылку') }
    finally { setBusy('') }
  }
  const choose = async (id: number, on: boolean) => {
    haptic.heavy()
    try { apply((await post<{ idea: Idea }>(`/ideas/${i.id}/options/${on ? id : 0}/choose`)).idea) } catch (e) { toast(humanError(e)) }
  }
  const remove = async (id: number) => { try { apply((await del<{ idea: Idea }>(`/ideas/${i.id}/options/${id}`)).idea) } catch (e) { toast(humanError(e)) } }
  const opts = i.options || []
  const who = (a: string) => a === 'ai' ? 'помощник' : a === me.color ? 'ты' : partner?.name || 'партнёр'
  return (
    <section className="places" data-testid="places">
      <div className="places__head">
        <h2 className="headline"><Icon n="pin" s={18} w={2.2} /> Где именно</h2>
        <button className="places__map" onClick={() => openLink(i.place?.maps || i.maps)}>Яндекс Карты <Icon n="link" s={14} w={2.2} /></button>
      </div>
      {i.generic && !opts.length && <p className="places__hint">«{i.title}» пока звучит общо. Добавь ссылку на конкретное место — я разберу название и адрес — или попроси помощника подобрать.</p>}
      <AnimatePresence initial={false}>
        {opts.map(o => (
          <motion.div key={o.id} layout initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, height: 0 }} className={'popt' + (o.chosen ? ' is-chosen' : '')} data-testid="place-option">
            <span className="popt__who" data-p={o.author}>{o.author === 'ai' ? <Icon n="spark" s={13} w={2.4} /> : ((o.author === me.color ? me.name : partner?.name) || '?').slice(0, 1).toUpperCase()}</span>
            <span className="popt__main">
              <b>{o.parsed ? o.title : <span className="popt__parsing"><span className="spin" />разбираю ссылку…</span>}</b>
              <span>{[o.address, o.price ? '≈ ' + money(o.price) : '', o.note || 'добавил(а) ' + who(o.author)].filter(Boolean).join(' · ')}</span>
              <span className="popt__links">
                <button onClick={() => openLink(o.maps)}>на карте</button>
                {o.url && !o.url.includes('yandex.ru/maps/?text=') && <button onClick={() => openLink(o.url!)}>ссылка</button>}
                {o.author === me.color && !o.chosen && <button onClick={() => remove(o.id)}>убрать</button>}
              </span>
            </span>
            <button className="popt__pick" aria-pressed={o.chosen} onClick={() => choose(o.id, !o.chosen)}>{o.chosen ? <><Icon n="check" s={15} w={2.6} />Берём</> : 'Берём этот'}</button>
          </motion.div>
        ))}
      </AnimatePresence>
      <form className="places__add" onSubmit={e => { e.preventDefault(); add() }}>
        <input className="input" value={text} onChange={e => setText(e.target.value)} placeholder="Ссылка на место или название" aria-label="Добавить вариант места" />
        <button className="places__send" type="submit" disabled={!text.trim() || !!busy} aria-label="Добавить вариант">{busy === 'add' ? <span className="spin" /> : <Icon n="plus" s={20} w={2.4} />}</button>
      </form>
      {!compact && <Btn kind="soft" block sm icon="spark" busy={busy === 'ai'} onClick={suggest} disabled={ai === false}>{ai === false ? 'Подбор мест — когда подключим AI' : 'Подобрать места'}</Btn>}
    </section>
  )
}
