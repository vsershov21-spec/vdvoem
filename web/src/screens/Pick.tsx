import { useMemo, useState } from 'react'
import type { Idea, Person, Wish } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { post, humanError } from '../lib/api'
import { haptic } from '../lib/tg'
import { ncase } from '../lib/format'
import { Btn, Empty, Segmented, Skel, SubHeader, toast } from '../ui/kit'
import { EventCard } from '../ui/EventCard'

type P = { id: number; title: string; ideas: Idea[]; by_me: boolean }

// A «подборка» from the assistant: everything that fit the request — not just the three in the chat.
// Mark what you like (it goes to the bank), send the whole list to the partner to mark theirs.
export function PickScreen({ id, partner }: { id: number; partner?: Person }) {
  const { data, set } = useLoad<P>('/picks/' + id)
  const [show, setShow] = useState<'all' | 'new' | 'ours'>('all')
  const [sort, setSort] = useState<'match' | 'date' | 'price'>('match')
  const list = useMemo(() => {
    const l = (data?.ideas || []).filter(i => show === 'all' || (show === 'new') === (i.source === 'scout' || i.source === 'channel') && !(i.my_wish || i.partner_marked))
    const at = (i: Idea) => i.starts_at || 9e12, pr = (i: Idea) => i.price_min || 9e12
    return sort === 'date' ? [...l].sort((a, b) => at(a) - at(b)) : sort === 'price' ? [...l].sort((a, b) => pr(a) - pr(b)) : l
  }, [data, show, sort])
  if (!data) return <><SubHeader title="Подборка" /><Skel h={150} /><Skel h={150} /></>
  const mark = async (i: Idea, w: Wish) => {
    haptic.tap()
    try {
      await post('/ideas/' + i.id + '/wish', { wish: w })
      set(d => d && { ...d, ideas: d.ideas.map(x => x.id === i.id ? { ...x, my_wish: w } : x) })
      emit('ideas'); emit('home')
      if (w !== 'skip') toast(w === 'very' ? 'Очень хочу ✓ — в копилке' : 'Хочу ✓ — в копилке')
    } catch (e) { toast(humanError(e)) }
  }
  const marked = data.ideas.filter(i => i.my_wish).length
  const send = async () => { try { await post(`/picks/${id}/send`); haptic.ok(); toast(`Отправил ${partner ? ncase(partner.name, 'dat') : 'партнёру'} — совпадения появятся у вас обоих`) } catch (e) { toast(humanError(e)) } }
  return (<>
    <SubHeader title="Подборка" />
    <div className="pick__head rise">
      <h1 className="title" style={{ margin: 0 }}>{data.title}</h1>
      <p className="caption muted" style={{ margin: '4px 0 0' }}>{data.ideas.length} вариантов · отмечено {marked}. Что отметишь «хочу» — попадёт в копилку.</p>
      {partner && <div style={{ marginTop: 10 }} data-testid="pick-send"><Btn kind="accent" block shine icon="send" onClick={send}>Отправить {ncase(partner.name, 'dat')} — пусть отметит</Btn></div>}
    </div>
    <div style={{ margin: '12px 0 8px' }}><Segmented<typeof show> items={[['all', 'Все'], ['new', '🆕 Новое'], ['ours', '🗂 Ваше']]} value={show} onChange={setShow} /></div>
    <div style={{ marginBottom: 10 }}><Segmented<typeof sort> items={[['match', 'По интересам'], ['date', 'По дате'], ['price', 'Дешевле']]} value={sort} onChange={setSort} /></div>
    {list.length === 0 ? <Empty title="Здесь пусто" text="Переключи на «Все»." /> :
      <div className="col">{list.map((i, k) => <EventCard key={i.id} i={i} k={k} onMark={mark} partnerName={partner?.name} />)}</div>}
  </>)
}
