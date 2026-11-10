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
import { Btn, Segmented, Skel, SubHeader, burst, toast } from '../ui/kit'
import { Particles, type Shape } from '../ui/Particles'

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
  const [shape, setShape] = useState<Shape>('sphere') // the cloud of points follows what you pick
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
  const tapSphere = (id: string) => {
    const w = next(t.spheres[id]); haptic.select(); if (w === 2) haptic.soft()
    setT({ ...t, spheres: { ...t.spheres, [id]: w } }); setShape(w > 0 ? `tint:${WORD[id] || id}` : w < 0 ? 'ring' : 'sphere')
  }
  const tapTag = (tag: string) => {
    const w = next(t.tags[tag]); haptic.select(); if (w === 2) haptic.soft()
    setT({ ...t, tags: { ...t.tags, [tag]: w } }); setShape(w > 0 ? `tint:${tag.length > 11 ? tag.split(' ')[0] : tag}` : w < 0 ? 'ring' : 'sphere')
  }
  const go2 = (s: 0 | 1 | 2 | 3) => { setStep(s); setShape(s === 3 ? 'heart' : s === 0 ? 'bridge' : 'sphere') }
  const toggle = (k: 'when' | 'style' | 'moods', v: string) => { haptic.select(); setT({ ...t, [k]: t[k].includes(v) ? t[k].filter(x => x !== v) : [...t[k], v] }) }

  return (<>
    <SubHeader title="Мой вкус" />
    <Hero shape={shape} step={step} cloud={d.cloud} />
    {step > 0 && <div className="steps rise" aria-label={`Шаг ${step} из 3`}>{[1, 2, 3].map(k => <i key={k} data-on={k <= step || undefined} />)}</div>}
    <AnimatePresence mode="wait">
      <motion.div key={step} initial={{ opacity: 0, x: 16 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: -16 }} transition={{ duration: 0.18 }} className="col" style={{ gap: 14 }}>
        {step === 0 && <Portrait d={d} onEdit={() => go2(1)} />}

        {step === 1 && <>
          <div><h2 className="title" style={{ margin: 0 }}>Что тебе нравится?</h2>
            <p className="caption muted" style={{ margin: '4px 0 0' }}>Тап — нравится, ещё раз — очень, ещё — не моё. Можно отметить всего пару.</p></div>
          <div className="tgrid">
            {d.spheres.map(s => (
              <motion.button key={s.id} className="ttile" data-w={t.spheres[s.id] || 0} onClick={() => tapSphere(s.id)} whileTap={{ scale: 0.94 }}
                animate={{ scale: t.spheres[s.id] === 2 ? 1.03 : 1 }} transition={{ type: 'spring', stiffness: 500, damping: 22 }} aria-pressed={!!t.spheres[s.id]} aria-label={s.title + (mark(t.spheres[s.id]) ? ', ' + word(t.spheres[s.id]) : '')}>
                <span className="ttile__e">{s.emoji}</span><span className="ttile__t">{s.title}</span>
                <AnimatePresence>{!!t.spheres[s.id] && <motion.span key={t.spheres[s.id]} className="ttile__m" initial={{ scale: 0.4, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} exit={{ scale: 0.4, opacity: 0 }}>{mark(t.spheres[s.id])}</motion.span>}</AnimatePresence>
              </motion.button>))}
          </div>
          <div className="row" style={{ gap: 8 }}>
            {!empty(d.me) && <Btn kind="ghost" onClick={() => go2(0)}>Отмена</Btn>}
            <Btn kind="primary" block busy={busy} disabled={!Object.values(t.spheres).some(w => w !== 0)} onClick={() => save(() => go2(liked.length ? 2 : 3))}>Дальше</Btn>
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
            <Btn kind="ghost" onClick={() => go2(1)}>Назад</Btn>
            <Btn kind="primary" block busy={busy} onClick={() => save(() => go2(3))}>Дальше</Btn>
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
            <Btn kind="ghost" onClick={() => go2(liked.length ? 2 : 1)}>Назад</Btn>
            <Btn kind="accent" block busy={busy} icon="check" onClick={() => save(() => { haptic.ok(); burst(); go2(0) })}>Готово</Btn>
          </div>
        </>}
      </motion.div>
    </AnimatePresence>
  </>)
}

const WORD: Record<string, string> = { theatre: 'театр', music: 'музыка', screen: 'кино', art: 'музеи', food: 'еда', walk: 'прогулки', active: 'движ', games: 'игры', create: 'творить', relax: 'отдых', show: 'шоу', sport: 'матчи' }
const STEP_LINE = ['', 'Что нравится вообще', 'А точнее', 'Как тебе лучше']

// the hero: the same cloud of points as the intro and the assistant — it becomes what you pick, and on the
// portrait it cycles through your strongest tags in our two colours
function Hero({ shape, step, cloud }: { shape: Shape; step: number; cloud: CloudTag[] }) {
  const top = useMemo(() => cloud.filter(x => x.said && x.w > 0).sort((a, b) => b.w - a.w).slice(0, 4).map(x => x.tag.length > 11 ? x.tag.split(' ')[0] : x.tag), [cloud])
  const [k, setK] = useState(0)
  useEffect(() => { if (step !== 0 || top.length < 2) return; const id = setInterval(() => setK(x => x + 1), 4200); return () => clearInterval(id) }, [step, top.length])
  const s: Shape = step === 0 ? (top.length ? `tint:${top[k % top.length]}` : 'bridge') : shape
  return (
    <div className="thero rise">
      <Particles from="sphere" shape={s} count={2600} scale={0.3} speed={1.4} />
      <span className="thero__line">{step === 0 ? (top.length ? 'Твой вкус' : 'Расскажи о себе') : `${step} / 3 · ${STEP_LINE[step]}`}</span>
    </div>
  )
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
  return <div className="tcloud">{tags.map((x, k) => (
    <motion.span key={x.tag} style={{ display: 'inline-block' }} initial={{ opacity: 0, y: 10, scale: 0.85 }} animate={{ opacity: 1, y: 0, scale: 1 }}
      transition={{ type: 'spring', stiffness: 380, damping: 24, delay: Math.min(k, 20) * 0.035 }}>
      <span className="tcloud__t" data-w={x.w} data-said={x.said || undefined} data-s={x.sphere} style={{ ['--f' as any]: (k % 5) * 0.6 + 's' }}>{x.w < 0 ? '✕ ' : ''}{x.tag}</span>
    </motion.span>))}</div>
}

function Facet({ title, children }: { title: string; children: React.ReactNode }) {
  return <div className="col" style={{ gap: 8 }}><span className="caption muted">{title}</span><div className="wkchips" style={{ flexWrap: 'wrap' }}>{children}</div></div>
}
function Chip({ on, onClick, children }: { on: boolean; onClick: () => void; children: React.ReactNode }) {
  return <button className="wkchip" data-on={on || undefined} aria-pressed={on} onClick={onClick}>{children}</button>
}
const word = (w?: number) => (w === 2 ? 'очень' : w === 1 ? 'нравится' : w === -1 ? 'не моё' : '')
const norm = (m: MyTaste): MyTaste => ({ spheres: m.spheres || {}, tags: m.tags || {}, when: m.when || [], budget: m.budget || '', pace: m.pace || '', style: m.style || [], scale: m.scale || '', moods: m.moods || [], note: m.note || '', at: m.at })
