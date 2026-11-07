import { useEffect, useRef, useState } from 'react'
import type { Idea } from '../lib/types'
import { post, humanError } from '../lib/api'
import { go } from '../lib/router'
import { Btn, Sheet, flyTo, toast } from '../ui/kit'
import { Icon } from '../ui/icons'

const examples = ['Ужин в грузинском ресторане', 'Каток в Парке Горького', 'Выставка в Гараже', 'Купить продукты на неделю']

export function AddSheet({ open, onClose, onAdded }: { open: boolean; onClose: () => void; onAdded: () => void }) {
  const [text, setText] = useState('')
  const [similar, setSimilar] = useState<{ idea: Idea; to: Idea } | null>(null)
  const ref = useRef<HTMLTextAreaElement>(null)
  const btn = useRef<HTMLDivElement>(null)
  useEffect(() => { if (open) { setText(''); setSimilar(null); setTimeout(() => ref.current?.focus(), 350) } }, [open])

  const paste = async () => {
    try { const t = await navigator.clipboard.readText(); if (t) setText(t.trim()) } catch { toast('Вставь текст вручную — доступ к буферу закрыт') }
  }
  const save = async () => {
    const t = text.trim()
    if (!t) return
    try {
      const r = await post<{ idea?: Idea; existing?: Idea; similar?: Idea }>('/ideas', { text: t })
      if (r.existing) { toast('Это уже есть в копилке: «' + r.existing.title + '»'); return }
      if (r.similar && r.idea) { setSimilar({ idea: r.idea, to: r.similar }); return }
      land(t)
    } catch (e) { toast(humanError(e)) }
  }
  const land = (t: string) => {
    const from = btn.current?.getBoundingClientRect()
    onClose()
    if (from) flyTo(t, from, '#tab-bank', onAdded); else onAdded()
  }
  const merge = async () => {
    if (!similar) return
    try { await post(`/ideas/${similar.idea.id}/merge`, { into: similar.to.id }); toast('Объединил с «' + similar.to.title + '»'); onClose(); onAdded() } catch (e) { toast(humanError(e)) }
  }

  return (
    <Sheet open={open} onClose={onClose}>
      {similar ? (
        <div className="col">
          <h2 className="title">Похоже, это уже есть</h2>
          <p className="callout muted" style={{ margin: 0 }}>В копилке лежит «{similar.to.title}». Это то же самое?</p>
          <Btn kind="primary" block onClick={merge}>Да, объединить</Btn>
          <Btn kind="soft" block onClick={() => land(similar.idea.title)}>Нет, оставить обе</Btn>
        </div>
      ) : (
        <div className="col">
          <div className="row"><h2 className="title">Новая идея</h2><span className="spacer" /><button className="iconbtn" onClick={paste} aria-label="Вставить"><Icon n="link" s={20} /></button></div>
          <textarea ref={ref} className="input" rows={3} maxLength={600} placeholder="Ссылка на место или событие — или просто напиши, куда хочется" value={text} onChange={e => setText(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); save() } }} />
          {!text && <div className="sugg">{examples.map(x => <button key={x} onClick={() => setText(x)}>{x}</button>)}</div>}
          <p className="caption muted" style={{ margin: 0 }}>По ссылке я сам достану название, адрес, цену и фото. Партнёр увидит идею, но не твою оценку.</p>
          <div ref={btn}><Btn kind="primary" block onClick={save} disabled={!text.trim()} icon="plus">В копилку</Btn></div>
          <button className="link" style={{ appearance: 'none', border: 0, background: 'none', color: 'var(--ink-muted)', font: '600 14px var(--font-sans)', cursor: 'pointer' }}
            onClick={() => { onClose(); go('assistant') }}>Не знаешь, что добавить? Спроси помощника</button>
        </div>
      )}
    </Sheet>
  )
}
