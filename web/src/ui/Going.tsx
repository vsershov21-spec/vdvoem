import type { Round } from '../lib/types'
import { go } from '../lib/router'
import { Icon } from './icons'
import { Cover } from './kit'

// «Мы идём»: everything both of you said yes to, still ahead — its own shelf, apart from ideas
export function Going({ list, title = 'Мы идём' }: { list: Round[]; title?: string }) {
  if (!list?.length) return null
  return (<>
    <div className="sec"><h2 className="title"><span className="orange-ind" style={{ background: 'var(--success)' }} />{title} · {list.length}</h2><button className="link" onClick={() => go('calendar')}>Календарь</button></div>
    <div className="rail" data-testid="going">
      {list.map((r, k) => {
        const i = r.chosen
        const when = r.starts_at ? new Date(r.starts_at * 1000).toLocaleString('ru', { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : ''
        return (
          <div key={r.id} className="icard rise" style={{ width: 220, ['--d' as any]: k }} onClick={() => go('round/' + r.id)}>
            <Cover idea={i || { title: 'Встреча', category: 'other' } as any} h={120}>
              <span>{when && <span className="pill pill--glass"><Icon n="cal" s={12} w={2.2} />{when}</span>}</span>
              <div className="cover__title" style={{ fontSize: 15, lineHeight: '19px' }}>{i?.title || 'Встреча'}</div>
            </Cover>
            <p className="caption" style={{ margin: '8px 4px 2px', color: r.state === 'planned' ? 'var(--success)' : 'var(--ink-muted)' }}>
              {r.state === 'planned' ? '✓ Договорились' : r.confirmed ? 'Ждём второе «да»' : 'Подтверди время'}</p>
          </div>)
      })}
    </div>
  </>)
}
