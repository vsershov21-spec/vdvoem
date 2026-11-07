// Idea folder — ported from svg_animations/FolderWidget: back plate, three cards that fan out of the pocket, curved pocket.
// Cards are the folder's ideas (photo or category cover). First tap fans the cards, second tap opens the folder.
import { useId, useState } from 'react'
import type { Idea } from '../lib/types'
import { CATS } from '../lib/format'
import { haptic } from '../lib/tg'
import { Icon } from './icons'

export type FolderTone = { back: [string, string, string]; pocket: [string, string, string]; glow: string }

export const TONES: Record<string, FolderTone> = {
  ember: { back: ['#ff9b6b', '#ef6537', '#d9471a'], pocket: ['#ffa77d', '#f2743f', '#e05522'], glow: 'rgba(224,77,29,.3)' },
  ocean: { back: ['#8aa8ff', '#4d7dff', '#2a55d4'], pocket: ['#9cb6ff', '#5d88ff', '#3561e0'], glow: 'rgba(42,85,212,.3)' },
  forest: { back: ['#6fd8a8', '#23a15b', '#12804a'], pocket: ['#7fe0b4', '#2fb06a', '#178a52'], glow: 'rgba(18,128,74,.3)' },
  gold: { back: ['#ffd84d', '#ffbb0e', '#f69d00'], pocket: ['#ffd034', '#ffbf12', '#f5a400'], glow: 'rgba(246,157,0,.3)' },
  rose: { back: ['#ff9db0', '#ff5a7a', '#e03a5e'], pocket: ['#ffb0c0', '#ff6d8a', '#ea4a6c'], glow: 'rgba(224,58,94,.3)' },
  onyx: { back: ['#55555c', '#2a2a2e', '#18181b'], pocket: ['#5e5e66', '#38383e', '#232327'], glow: 'rgba(0,0,0,.35)' },
}

const TONE_GRAD: Record<string, string> = {
  ember: 'linear-gradient(140deg,#f57d4c,#ef6537 55%,#d9471a)', ocean: 'linear-gradient(140deg,#6d93ff,#3d6ef5 55%,#2a55d4)',
  forest: 'linear-gradient(140deg,#43c890,#14a06a 55%,#0c8253)', onyx: 'linear-gradient(140deg,#34343a,#1e1e20)',
}

function Mini({ idea, cls }: { idea?: Idea; cls: string }) {
  if (!idea) return <div className={'fcard ' + cls + ' fcard--empty'} />
  const cat = CATS[idea.category] || CATS.other
  return (
    <div className={'fcard ' + cls} style={{ background: idea.image ? '#111' : TONE_GRAD[cat.tone] }}>
      {idea.image ? <img src={idea.image} alt="" loading="lazy" referrerPolicy="no-referrer" /> : <Icon n={idea.category} s={120} w={1.3} className="fcard__glyph" />}
      <span className="fcard__title">{idea.title}</span>
    </div>
  )
}

export function Folder({ label, count, ideas, tone = 'gold', icon, onOpen, d = 0 }: {
  label: string; count: number; ideas: Idea[]; tone?: keyof typeof TONES; icon?: React.ReactNode; onOpen: () => void; d?: number
}) {
  const [fan, setFan] = useState(false)
  const id = useId().replace(/:/g, '')
  const t = TONES[tone]
  const tap = () => {
    if (!fan && ideas.length) { haptic.soft(); setFan(true); setTimeout(() => setFan(false), 2600); return }
    haptic.tap(); onOpen()
  }
  return (
    <button className="folder" style={{ ['--d' as any]: d }} onClick={tap} onPointerEnter={e => { if (e.pointerType === 'mouse' && matchMedia('(hover: hover)').matches) setFan(true) }} onPointerLeave={e => { if (e.pointerType === 'mouse') setFan(false) }} aria-label={`${label}: ${count}`}>
      <div className="folder__stage">
        <div className={'folder__wrap' + (fan ? ' is-fan' : '')}>
          <div className="folder__back" style={{ background: `linear-gradient(145deg,${t.back[0]} 0%,${t.back[1]} 50%,${t.back[2]} 100%)`, boxShadow: `0 30px 60px ${t.glow},0 15px 35px rgba(0,0,0,.15),inset 0 2px 4px rgba(255,255,255,.6)` }}>
            <div className="folder__shine" />
          </div>
          <div className="folder__cards">
            <Mini idea={ideas[2]} cls="fcard--l" />
            <Mini idea={ideas[1]} cls="fcard--m" />
            <Mini idea={ideas[0]} cls="fcard--r" />
          </div>
          <div className="folder__pocket">
            <svg viewBox="0 0 340 215" preserveAspectRatio="none">
              <defs><linearGradient id={'pg' + id} x1="0%" y1="0%" x2="0%" y2="100%"><stop offset="0%" stopColor={t.pocket[0]} /><stop offset="40%" stopColor={t.pocket[1]} /><stop offset="100%" stopColor={t.pocket[2]} /></linearGradient></defs>
              <path d="M 0 35 Q 0 0 35 0 L 130 0 Q 155 0 175 22 L 185 32 Q 205 52 230 52 L 305 52 Q 340 52 340 87 L 340 175 Q 340 215 300 215 L 40 215 Q 0 215 0 175 Z" fill={`url(#pg${id})`} />
              <path d="M 0 35 Q 0 0 35 0 L 130 0 Q 155 0 175 22 L 185 32 Q 205 52 230 52 L 305 52 Q 340 52 340 87" fill="none" stroke="rgba(255,255,255,.65)" strokeWidth="2.5" />
            </svg>
            <span className="folder__count">{count}</span>
            {icon && <span className="folder__icon">{icon}</span>}
          </div>
        </div>
      </div>
      <span className="folder__label">{label}</span>
    </button>
  )
}
