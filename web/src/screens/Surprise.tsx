import { useEffect, useState } from 'react'
import { startWith } from '../lib/startWith'
import type { Idea } from '../lib/types'
import { get, humanError } from '../lib/api'
import { haptic } from '../lib/tg'
import { Btn, Cover, Segmented, Sheet, toast } from '../ui/kit'
import { Particles } from '../ui/Particles'

type S = { idea: Idea; why: string; mode: string }
type From = 'ours' | 'afisha' | 'all'
const FROM_WORD: Record<From, string> = { ours: 'из копилки', afisha: 'из афиши', all: 'отовсюду' }

// «Удиви нас» / «Что посоветуешь?»: one idea, picked by the server (weighted random or best score).
export function SurpriseSheet({ mode, onClose }: { mode: '' | 'random' | 'best'; onClose: () => void }) {
  const [s, setS] = useState<S | null>(null)
  const [chain, setChain] = useState<number[]>([])
  const [rolling, setRolling] = useState(false)
  // where to pick from: our own ideas, what the scout and channels found, or everything
  const [from, setFromRaw] = useState<From>(() => { try { return (localStorage.getItem('vd-from') as From) || 'ours' } catch { return 'ours' } })
  const setFrom = (f: From) => { setFromRaw(f); try { localStorage.setItem('vd-from', f) } catch { /* private */ } setChain([]); pull(s?.mode === 'random' || mode === 'random' ? 'random' : 'best', [], f) }
  const pull = async (m: string, ex: number[], f: From = from) => {
    setRolling(true); haptic.heavy()
    try {
      const [r] = await Promise.all([get<S>(`/suggest?mode=${m}&from=${f}&exclude=${ex.join(',')}`), new Promise(res => setTimeout(res, 900))])
      setS(r); setChain([...ex, r.idea.id]); haptic.ok()
    } catch (e) { toast(ex.length ? 'Больше подходящих идей нет.' : f !== 'all' ? 'Здесь пусто — попробуй «Отовсюду»' : humanError(e)); if (!ex.length && f === 'all') onClose() }
    finally { setRolling(false) }
  }
  useEffect(() => { if (mode) { setS(null); setChain([]); pull(mode, []) } }, [mode])
  const goWith = async () => {
    if (await startWith(s!.idea.id, s!.idea.title)) onClose()
  }
  return (
    <Sheet open={!!mode} onClose={onClose}>
      <div style={{ marginBottom: 12 }} data-testid="from"><Segmented<From> items={[['ours', 'Наши идеи'], ['afisha', 'Что нашёл скаут'], ['all', 'Отовсюду']]} value={from} onChange={setFrom} /></div>
      {(rolling || !s) ? (
        <div className="col" style={{ alignItems: 'center', padding: '6px 0 20px' }}>
          <div className="orb-canvas"><Particles shape="sphere" count={1600} scale={0.36} interactive={false} /></div>
          <p className="callout muted" style={{ margin: 0 }}>{mode === 'random' ? `Кручу ${FROM_WORD[from]}…` : 'Сравниваю ваши оценки…'}</p>
        </div>
      ) : (
        <div className="col" key={s.idea.id}>
          <p className="micro muted" style={{ margin: 0 }}>{s.mode === 'random' ? `🎲 Случайно ${FROM_WORD[from]}` : `Мой совет ${FROM_WORD[from]}`}</p>
          <div className="rise"><Cover idea={s.idea} h={200} big><span /><div className="cover__title" style={{ fontSize: 22, lineHeight: '26px' }}>{s.idea.title}</div></Cover></div>
          <p className="caption muted" style={{ margin: 0 }}>{s.idea.meta}</p>
          <p className="callout" style={{ margin: 0 }}>{s.why}</p>
          <Btn kind="accent" block shine onClick={goWith} icon="check">Давай!</Btn>
          <div className="row"><Btn kind="soft" block onClick={() => pull('best', chain)}>Другую</Btn><Btn kind="soft" block icon="dice" onClick={() => pull('random', chain)}>Рандом</Btn></div>
        </div>
      )}
    </Sheet>
  )
}
