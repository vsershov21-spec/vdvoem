// «Мой вкус»: like the first open of a music app — what you like at all, then the kinds inside, then how.
// Each level is optional and saves as you go; the result is a cloud of tags that the marks keep refining.
// Tap: нравится → очень → не моё → снова пусто.
import { useEffect, useMemo, useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { get, put, humanError } from '../lib/api'
import { emit } from '../lib/data'
import { go } from '../lib/router'
import { haptic } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Btn, Segmented, Skel, SubHeader, toast } from '../ui/kit'

type Sphere = { id: string; title: string; emoji: string; tags: string[] }
type MyTaste = { spheres: Record<string, number>; tags: Record<string, number>; when: string[]; budget: string; pace: string; style: string[]; scale: string; moods: string[]; note: string; at?: number }
type CloudTag = { tag: string; w: number; said: boolean; sphere: string }
type Data = { spheres: Sphere[]; me: MyTaste; cloud: CloudTag[]; partner?: { name: string; cloud: CloudTag[] } }

const next = (w: number | undefined) => (w === 1 ? 2 : w === 2 ? -1 : w === -1 ? 0 : 1)
const mark = (w?: number) => (w === 2 ? '♥♥' : w === 1 ? '♥' : w === -1 ? '✕' : '')
const WHEN: [string, string][] = [['weekday_evening', 'Вечера будней'], ['weekend_day', 'Днём в выходные'], ['weekend_evening', 'Вечера выходных'], ['morning', 'Утро']]
const STYLE: [string, string][] = [['classic', 'Классика'], ['modern', 'Современное'], ['experimental', 'Эксперименты']]
const MOODS: [string, string][] = [['romantic', '💞 Романтика'], ['fun', '😂 Посмеяться'], ['new', '✨ Новое'], ['adrenaline', '⚡ Адреналин'], ['chill', '🫖 Отдохнуть']]
const empty = (t?: MyTaste) => !t || (!Object.keys(t.spheres || {}).length && !Object.keys(t.tags || {}).length)

export function TasteScreen() {
  const [d, setD] = useState<Data | null>(null)
  const [t, setT] = useState<MyTaste | null>(null)
  const [step, setStep] = useState<0 | 1 | 2 | 3>(0) // 0 — the portrait, 1–3 — the levels
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    get<Data>('/taste').then(x => { setD(x); setT(norm(x.me)); if (empty(x.me)) setStep(1) }).catch(e => toast(humanError(e)))
  }, [])
  const liked = useMemo(() => (d && t ? d.spheres.filter(s => (t.spheres[s.id] || 0) > 0) : []), [d, t])
  if (!d || !t) return <><SubHeader title="Мой вкус" /><Skel h={120} /><Skel h={320} /></>

  const save = async (then: () => void) => {
    setBusy(true)
    try { const r = await put<{ me: MyTaste; cloud: CloudTag[] }>('/taste', t); setT(norm(r.me)); setD({ ...d, me: r.me, cloud: r.cloud }); emit('home'); then() }
    catch (e) { toast(humanError(e)) } finally { setBusy(false) }
  }
  const tapSphere = (id: string) => { haptic.select(); setT({ ...t, spheres: { ...t.spheres, [id]: next(t.spheres[id]) } }) }
  const tapTag = (tag: string) => { haptic.select(); setT({ ...t, tags: { ...t.tags, [tag]: next(t.tags[tag]) } }) }
  const toggle = (k: 'when' | 'style' | 'moods', v: string) => { haptic.select(); setT({ ...t, [k]: t[k].includes(v) ? t[k].filter(x => x !== v) : [...t[k], v] }) }

  return (<>
    <SubHeader title="Мой вкус" />
    {step > 0 && <div className="steps rise" aria-label={`Шаг ${step} из 3`}>{[1, 2, 3].map(k => <i key={k} data-on={k <= step || undefined} />)}</div>}
    <AnimatePresence mode="wait">
      <motion.div key={step} initial={{ opacity: 0, x: 16 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: -16 }} transition={{ duration: 0.18 }} className="col" style={{ gap: 14 }}>
        {step === 0 && <Portrait d={d} onEdit={() => setStep(1)} />}

        {step === 1 && <>
          <div><h2 className="title" style={{ margin: 0 }}>Что тебе нравится?</h2>
            <p className="caption muted" style={{ margin: '4px 0 0' }}>Тап — нравится, ещё раз — очень, ещё — не моё. Можно отметить всего пару.</p></div>
          <div className="tgrid">
            {d.spheres.map(s => (
              <button key={s.id} className="ttile" data-w={t.spheres[s.id] || 0} onClick={() => tapSphere(s.id)} aria-pressed={!!t.spheres[s.id]} aria-label={s.title + (mark(t.spheres[s.id]) ? ', ' + word(t.spheres[s.id]) : '')}>
                <span className="ttile__e">{s.emoji}</span><span className="ttile__t">{s.title}</span>
                {!!t.spheres[s.id] && <span className="ttile__m">{mark(t.spheres[s.id])}</span>}
              </button>))}
          </div>
          <div className="row" style={{ gap: 8 }}>
            {!empty(d.me) && <Btn kind="ghost" onClick={() => setStep(0)}>Отмена</Btn>}
            <Btn kind="primary" block busy={busy} disabled={!Object.values(t.spheres).some(w => w !== 0)} onClick={() => save(() => setStep(liked.length ? 2 : 3))}>Дальше</Btn>
          </div>
        </>}

        {step === 2 && <>
          <div><h2 className="title" style={{ margin: 0 }}>А точнее?</h2>
            <p className="caption muted" style={{ margin: '4px 0 0' }}>Отметь, что ближе, — или пропусти: я научусь по твоим отметкам.</p></div>
          {liked.map(s => (
            <section key={s.id} className="col" style={{ gap: 8 }}>
              <b className="headline">{s.emoji} {s.title}</b>
              <div className="wkchips" style={{ flexWrap: 'wrap' }}>
                {s.tags.map(tag => <button key={tag} className="wkchip tchip" data-w={t.tags[tag] || 0} onClick={() => tapTag(tag)} aria-pressed={!!t.tags[tag]}>{tag}{!!t.tags[tag] && <b>{mark(t.tags[tag])}</b>}</button>)}
              </div>
            </section>))}
          <div className="row" style={{ gap: 8 }}>
            <Btn kind="ghost" onClick={() => setStep(1)}>Назад</Btn>
            <Btn kind="primary" block busy={busy} onClick={() => save(() => setStep(3))}>Дальше</Btn>
          </div>
        </>}

        {step === 3 && <>
          <div><h2 className="title" style={{ margin: 0 }}>Как тебе лучше?</h2>
            <p className="caption muted" style={{ margin: '4px 0 0' }}>По желанию — так подборки попадут ещё точнее.</p></div>
          <Facet title="Когда удобно">{WHEN.map(([k, l]) => <Chip key={k} on={t.when.includes(k)} onClick={() => toggle('when', k)}>{l}</Chip>)}</Facet>
          <Facet title="Бюджет на двоих"><Segmented items={[['2000', 'до 2 т'], ['5000', 'до 5 т'], ['10000', 'до 10 т'], ['any', 'не важно']]} value={t.budget || 'any'} onChange={v => setT({ ...t, budget: v })} /></Facet>
          <Facet title="Темп"><Segmented items={[['calm', 'Спокойно'], ['mix', 'По-разному'], ['active', 'Активно']]} value={t.pace || 'mix'} onChange={v => setT({ ...t, pace: v })} /></Facet>
          <Facet title="Формат"><Segmented items={[['intimate', 'Камерно'], ['any', 'Любой'], ['big', 'Масштабно']]} value={t.scale || 'any'} onChange={v => setT({ ...t, scale: v })} /></Facet>
          <Facet title="Ближе по духу">{STYLE.map(([k, l]) => <Chip key={k} on={t.style.includes(k)} onClick={() => toggle('style', k)}>{l}</Chip>)}</Facet>
          <Facet title="Чего чаще хочется">{MOODS.map(([k, l]) => <Chip key={k} on={t.moods.includes(k)} onClick={() => toggle('moods', k)}>{l}</Chip>)}</Facet>
          <Facet title="Своими словами">
            <textarea className="input" rows={2} maxLength={300} placeholder="Например: люблю камерные концерты, не люблю очереди и громкую музыку" value={t.note} onChange={e => setT({ ...t, note: e.target.value })} style={{ width: '100%', resize: 'none' }} />
          </Facet>
          <div className="row" style={{ gap: 8 }}>
            <Btn kind="ghost" onClick={() => setStep(liked.length ? 2 : 1)}>Назад</Btn>
            <Btn kind="accent" block busy={busy} icon="check" onClick={() => save(() => { haptic.ok(); setStep(0) })}>Готово</Btn>
          </div>
        </>}
      </motion.div>
    </AnimatePresence>
  </>)
}

function Portrait({ d, onEdit }: { d: Data; onEdit: () => void }) {
  return (<>
    <div className="card rise col" style={{ gap: 12 }}>
      <div className="row" style={{ alignItems: 'baseline' }}><b className="headline">Твой вкус</b><span className="spacer" />
        <button className="link" onClick={onEdit}><Icon n="edit" s={14} />Изменить</button></div>
      <Cloud tags={d.cloud} />
      <p className="caption muted" style={{ margin: 0 }}>Яркие — твои слова, светлые — я заметил по отметкам. Подборки, скаут и помощник учитывают всё это сразу.</p>
    </div>
    {d.partner && d.partner.cloud.length > 0 && <div className="card rise col" style={{ gap: 12, ['--d' as any]: 1 }}>
      <b className="headline">Вкус {d.partner.name}</b>
      <Cloud tags={d.partner.cloud} />
      <p className="caption muted" style={{ margin: 0 }}>Подсказка для сюрприза 😉</p>
    </div>}
    <Btn kind="soft" block icon="spark" onClick={() => go('assistant')}>Что мне понравится на неделе?</Btn>
  </>)
}

function Cloud({ tags }: { tags: CloudTag[] }) {
  if (!tags.length) return <p className="callout muted" style={{ margin: 0 }}>Пока пусто — отметь, что нравится.</p>
  return <div className="tcloud">{tags.map(x => <span key={x.tag} className="tcloud__t" data-w={x.w} data-said={x.said || undefined} data-s={x.sphere}>{x.w < 0 ? '✕ ' : ''}{x.tag}</span>)}</div>
}

function Facet({ title, children }: { title: string; children: React.ReactNode }) {
  return <div className="col" style={{ gap: 8 }}><span className="caption muted">{title}</span><div className="wkchips" style={{ flexWrap: 'wrap' }}>{children}</div></div>
}
function Chip({ on, onClick, children }: { on: boolean; onClick: () => void; children: React.ReactNode }) {
  return <button className="wkchip" data-on={on || undefined} aria-pressed={on} onClick={onClick}>{children}</button>
}
const word = (w?: number) => (w === 2 ? 'очень' : w === 1 ? 'нравится' : w === -1 ? 'не моё' : '')
const norm = (m: MyTaste): MyTaste => ({ spheres: m.spheres || {}, tags: m.tags || {}, when: m.when || [], budget: m.budget || '', pace: m.pace || '', style: m.style || [], scale: m.scale || '', moods: m.moods || [], note: m.note || '', at: m.at })
