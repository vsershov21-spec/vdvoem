import { useCallback, useMemo, useState } from 'react'
import type { Home, Idea } from '../lib/types'
import { useLoad, useOn } from '../lib/data'
import { go } from '../lib/router'
import { CATS } from '../lib/format'
import { Empty, IdeaTile, Segmented, Skel } from '../ui/kit'

type Tab = 'leisure' | 'chore' | 'archived'

export function Ideas({ home }: { home: Home | null }) {
  const [tab, setTab] = useState<Tab>('leisure')
  const [cat, setCat] = useState('')
  const path = tab === 'archived' ? '/ideas?status=archived' : '/ideas?kind=' + tab
  const { data, refresh } = useLoad<{ ideas: Idea[] }>(path, 25000)
  useOn('ideas', useCallback(() => { refresh() }, [refresh]))
  const ideas = useMemo(() => (data?.ideas || []).filter(i => !cat || i.category === cat), [data, cat])
  const cats = useMemo(() => Array.from(new Set((data?.ideas || []).map(i => i.category))), [data])
  return (
    <>
      <header className="hdr"><h1 className="display">Копилка</h1></header>
      <Segmented<Tab> items={[['leisure', 'Досуг'], ['chore', 'Дела'], ['archived', 'Архив']]} value={tab} onChange={v => { setTab(v); setCat('') }} />
      {tab === 'leisure' && cats.length > 1 && (
        <div className="sugg" style={{ marginTop: 12 }}>
          <button onClick={() => setCat('')} style={!cat ? { background: 'var(--solid)', color: 'var(--on-solid)', borderColor: 'var(--solid)' } : undefined}>Все</button>
          {cats.map(c => <button key={c} onClick={() => setCat(c)} style={cat === c ? { background: 'var(--solid)', color: 'var(--on-solid)', borderColor: 'var(--solid)' } : undefined}>{CATS[c]?.name || c}</button>)}
        </div>
      )}
      <div style={{ height: 14 }} />
      {!data || !home ? <div className="grid">{[0, 1, 2, 3].map(k => <Skel key={k} h={170} />)}</div>
        : ideas.length === 0 ? <Empty title={tab === 'chore' ? 'Дел нет' : tab === 'archived' ? 'Архив пуст' : 'Копилка пуста'}
          text={tab === 'chore' ? 'Напиши боту «купить продукты» или «записаться к врачу» — дело появится здесь.' : 'Пришли боту ссылку на ресторан, выставку или просто напиши идею.'} />
        : <div className="grid">{ideas.map((i, k) => <IdeaTile key={i.id} idea={i} me={home.me} d={k} onOpen={() => go('idea/' + i.id)} />)}</div>}
    </>
  )
}
