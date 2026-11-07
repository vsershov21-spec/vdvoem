import { useEffect, useState } from 'react'
import type { Idea, Round } from '../lib/types'
import { get, post, humanError } from '../lib/api'
import { emit } from '../lib/data'
import { go } from '../lib/router'
import { haptic } from '../lib/tg'
import { Btn, Cover, Sheet, toast } from '../ui/kit'
import { Particles } from '../ui/Particles'

type S = { idea: Idea; why: string; mode: string }

// «Удиви нас» / «Что посоветуешь?»: one idea, picked by the server (weighted random or best score).
export function SurpriseSheet({ mode, onClose }: { mode: '' | 'random' | 'best'; onClose: () => void }) {
  const [s, setS] = useState<S | null>(null)
  const [chain, setChain] = useState<number[]>([])
  const [rolling, setRolling] = useState(false)
  const pull = async (m: string, ex: number[]) => {
    setRolling(true); haptic.heavy()
    try {
      const [r] = await Promise.all([get<S>(`/suggest?mode=${m}&exclude=${ex.join(',')}`), new Promise(res => setTimeout(res, 900))])
      setS(r); setChain([...ex, r.idea.id]); haptic.ok()
    } catch (e) { toast(ex.length ? 'Больше подходящих идей нет.' : humanError(e)); if (!ex.length) onClose() }
    finally { setRolling(false) }
  }
  useEffect(() => { if (mode) { setS(null); setChain([]); pull(mode, []) } }, [mode])
  const goWith = async () => {
    try { const r = await post<{ round: Round }>('/rounds/with/' + s!.idea.id); onClose(); emit('home'); go('round/' + r.round.id) } catch (e) { toast(humanError(e)) }
  }
  return (
    <Sheet open={!!mode} onClose={onClose}>
      {(rolling || !s) ? (
        <div className="col" style={{ alignItems: 'center', padding: '6px 0 20px' }}>
          <div className="orb-canvas"><Particles shape="sphere" count={1600} scale={0.36} interactive={false} /></div>
          <p className="callout muted" style={{ margin: 0 }}>{mode === 'random' ? 'Кручу копилку…' : 'Сравниваю ваши оценки…'}</p>
        </div>
      ) : (
        <div className="col" key={s.idea.id}>
          <p className="micro muted" style={{ margin: 0 }}>{s.mode === 'random' ? '🎲 Случайно из копилки' : 'Мой совет'}</p>
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
