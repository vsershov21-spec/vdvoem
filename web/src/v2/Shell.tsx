import { useEffect, useRef, useState } from 'react'
import './v2.css'
import { useRoute, back, navDir, go } from '../lib/router'
import { post } from '../lib/api'
import { setBack } from '../lib/tg'
import { useLoad, useOn } from '../lib/data'
import type { HomeV2 } from './types'
import { Nav, Toast, TABS, Empty } from './ui'
import { Home } from './Home'
import { Ideas, IdeaCard, AddIdea, PickList, showIdeas } from './Ideas'
import { Invite } from './Invite'
import { Meet } from './Meet'
import { Plans, Free } from './Plans'
import { Chores, ChoreCard } from './Chores'
import { Profile, History, Settings, Taste } from './Profile'
import { Assistant } from './Assistant'
import { particles, still } from './particles'

// The shell of the new interface: five tabs with the assistant in the middle, nested screens that go back one
// step at a time («‹ откуда пришёл»), one confirmation line, and both themes. Screens that are not built yet
// say so honestly instead of pretending.
const NAMES: Record<string, string> = { '': 'Главная', ideas: 'Идеи', assistant: 'Помощник', chores: 'Дела', plans: 'Планы', idea: 'Идея', chore: 'Дело', meet: 'Встреча', us: 'Профиль', settings: 'Настройки', history: 'История', free: 'Общее время', taste: 'Мой вкус', invite: 'Приглашение', pick: 'Подборка' }
const STEP: Record<string, number> = { assistant: 7 }

// The bot's buttons and old links still name the screens of the previous interface. Each of them opens the
// screen that took its place: a round is a meeting, the calendar is the plans, the folders of ideas are views
// of «Идеи», chores have their own tab, the old «Разбор» is the deck on the home screen.
export function legacy(route: string[]): string | null {
  const [top, sub] = route
  if (top === 'round') return Number(sub) > 0 ? 'meet/' + sub : 'plans'
  if (top === 'calendar') return 'plans'
  if (top === 'triage') return ''
  if (top === 'ideas' && sub) {
    if (sub === 'chores') return 'chores'
    if (sub === 'scout') showIdeas('afisha')
    else if (sub === 'both') showIdeas('both')
    return 'ideas'
  }
  return null
}
let told = false

export function Shell() {
  const raw = useRoute()
  const old = legacy(raw)
  const route = old === null ? raw : old ? old.split('/') : ['']
  useEffect(() => { if (old !== null) go(old, true) }, [old])
  // the server learns that this person has the new interface: the bot then writes in its flow
  useEffect(() => { if (!told) { told = true; post('/ui', { v: 2 }).catch(() => { told = false }) } }, [])
  const key = route.join('/')
  const top = route[0]
  const home = useLoad<HomeV2>('/home', 20000)
  useOn('home', home.refresh)
  const [adding, setAdding] = useState(false)
  const [dark, setDark] = useState(document.documentElement.dataset.theme === 'dark')
  useEffect(() => {
    const o = new MutationObserver(() => setDark(document.documentElement.dataset.theme === 'dark'))
    o.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
    return () => o.disconnect()
  }, [])

  // the trail: which screens lead here — the back label is the previous one, and the tab stays lit
  const trail = useRef<string[]>([key])
  const prev = useRef(key)
  let anim = ''
  if (prev.current !== key) {
    const isTab = TABS.some(t => t.path === key)
    const wasTab = TABS.some(t => t.path === prev.current)
    if (isTab) { trail.current = [key]; anim = wasTab ? 't-fade' : 't-pop' }
    else if (navDir() === 'back' && trail.current.includes(key)) { trail.current = trail.current.slice(0, trail.current.lastIndexOf(key) + 1); anim = 't-pop' }
    else { trail.current.push(key); anim = 't-push' }
    prev.current = key
  }
  const rootTab = TABS.find(t => t.path === trail.current[0])?.path ?? ''
  const nested = !TABS.some(t => t.path === key)
  const from = NAMES[(trail.current[trail.current.length - 2] || '').split('/')[0]] || 'Главная'
  useEffect(() => { setBack(nested ? () => back('') : null); return () => setBack(null) }, [nested, key])

  const hot = (home.data?.inbox || []).filter(r => r.hot).length
  const ch = home.data?.chore_summary
  let screen
  if (top === '') screen = <Home home={home.data} onAddIdea={() => setAdding(true)} />
  else if (top === 'ideas') screen = <Ideas home={home.data} onAdd={() => setAdding(true)} />
  else if (top === 'idea') screen = <IdeaCard id={Number(route[1])} home={home.data} from={from} />
  else if (top === 'meet') screen = <Meet id={Number(route[1])} home={home.data} from={from} />
  else if (top === 'plans') screen = <Plans home={home.data} />
  else if (top === 'free') screen = <Free home={home.data} from={from} />
  else if (top === 'assistant') screen = <Assistant />
  else if (top === 'chores') screen = <Chores home={home.data} />
  else if (top === 'chore') screen = <ChoreCard id={Number(route[1])} home={home.data} from={from} />
  else if (top === 'us') screen = <Profile home={home.data} from={from} />
  else if (top === 'history') screen = <History view={route[1] || 'meets'} home={home.data} from={from} />
  else if (top === 'settings') screen = <Settings section={route[1]} home={home.data} from={from} />
  else if (top === 'taste') screen = <Taste from={from} />
  else if (top === 'invite') screen = <Invite code={route[1] || ''} />
  else if (top === 'pick') screen = <PickList id={Number(route[1])} from={from} />
  else screen = (
    <div className="scr" data-testid={'v2-' + top}>
      {nested ? <div className="subh"><button className="back" aria-label="Назад" onClick={() => back('')}>‹ {from}</button><b>{NAMES[top] || ''}</b><span /></div> : <h2>{NAMES[top]}</h2>}
      <Empty title="Экран собирается"><p className="m">Шаг {STEP[top] || 7} из 7. Сервер под него уже готов.</p></Empty>
    </div>)
  return (
    <div className={'v2' + (dark ? ' dark' : '')} data-testid="v2">
      <div key={key} className={'v2-wrap ' + anim}>{screen}</div>
      <Nav tab={rootTab} counts={{ '': hot, chores: ch ? ch.today + ch.overdue : 0 }} />
      <Toast />
      <Splash />
      <AddIdea open={adding} onClose={() => setAdding(false)} />
    </div>)
}

// the loading mark: dots gather into two rings, once per launch
let splashed = false
function Splash() {
  const [on, setOn] = useState(!splashed)
  const [out, setOut] = useState(false)
  const cv = useRef<HTMLCanvasElement>(null)
  useEffect(() => {
    if (!on) return
    splashed = true
    if (cv.current) particles(cv.current, 'rings', 1300)
    const a = setTimeout(() => setOut(true), still() ? 500 : 1900), b = setTimeout(() => setOn(false), still() ? 800 : 2400)
    return () => { clearTimeout(a); clearTimeout(b) }
  }, [on])
  if (!on) return null
  return <div className={'splash' + (out ? ' out' : '')} aria-hidden="true"><canvas ref={cv} width={440} height={340} /><b>Вдвоём</b></div>
}
