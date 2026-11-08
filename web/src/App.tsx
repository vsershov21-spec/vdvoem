import { lazy, Suspense, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useRoute, go, navDir, back } from './lib/router'
import { scheme, onTheme, setBack, statusBar } from './lib/tg'
import { useLoad, emit, useOn, prefetch } from './lib/data'
import { get, post, humanError } from './lib/api'
import { startLive } from './lib/live'
import type { Home as HomeT, Round } from './lib/types'
import { Burst, Toasts, toast } from './ui/kit'
import { PillNav, Island, TABS } from './ui/Nav'
import { Entry } from './screens/Entry'
import { Home } from './screens/Home'
import { Ideas } from './screens/Ideas'
import { AddSheet } from './screens/AddSheet'

// screens off the first paint load as separate chunks, fetched in the background right after start
const load = {
  idea: () => import('./screens/Idea'), round: () => import('./screens/Round'), assistant: () => import('./screens/Assistant'),
  calendar: () => import('./screens/Calendar'), us: () => import('./screens/Us'), surprise: () => import('./screens/Surprise'),
  triage: () => import('./screens/Triage'),
}
const IdeaScreen = lazy(() => load.idea().then(m => ({ default: m.IdeaScreen })))
const RoundScreen = lazy(() => load.round().then(m => ({ default: m.RoundScreen })))
const Assistant = lazy(() => load.assistant().then(m => ({ default: m.Assistant })))
const CalendarScreen = lazy(() => load.calendar().then(m => ({ default: m.CalendarScreen })))
const Us = lazy(() => load.us().then(m => ({ default: m.Us })))
const Triage = lazy(() => load.triage().then(m => ({ default: m.Triage })))
const SurpriseSheet = lazy(() => load.surprise().then(m => ({ default: m.SurpriseSheet })))

let themePref = ''
export const setThemePref = (t: string) => { themePref = t; applyTheme() }
function applyTheme() {
  const s = themePref === 'light' || themePref === 'dark' ? themePref : scheme()
  document.documentElement.dataset.theme = s
  document.querySelector('meta[name=theme-color]')?.setAttribute('content', s === 'dark' ? '#0f0f11' : '#f4f4f4')
  statusBar(s === 'dark')
}

export function App() {
  const [entered, setEntered] = useState(false)
  useEffect(() => { applyTheme(); onTheme(applyTheme) }, [])
  return (<>
    {entered && <Main />}
    {!entered && <Entry onEnter={() => setEntered(true)} />}
    <Burst />
    <Toasts />
  </>)
}

function Main() {
  const route = useRoute()
  const [adding, setAdding] = useState(false)
  const [surprise, setSurprise] = useState<'' | 'random' | 'best'>('')
  const home = useLoad<HomeT>('/home', 20000)
  useOn('home', home.refresh)
  useEffect(() => {
    get_me_theme(); startLive()
    // warm the other tabs, so the first switch to them is instant too
    const m = new Date(), ym = `${m.getFullYear()}-${String(m.getMonth() + 1).padStart(2, '0')}`
    const t = setTimeout(() => {
      prefetch(['/ideas', '/ideas?status=archived', '/stats', '/calendar?month=' + ym, '/history', '/settings'])
      Object.values(load).forEach(f => f().catch(() => {}))
    }, 600)
    return () => clearTimeout(t)
  }, [])

  const top = route[0]
  const tab = TABS.find(t => t.path === top)
  const key = route.join('/')
  // transitions: tab → tab slides toward the tab's side, push/back slide like iOS; a screen seen before
  // skips its staggered entrance (its data is cached, nothing to «arrive»)
  const prevKey = useRef(key), seen = useRef(new Set<string>()), scrolls = useRef(new Map<string, number>())
  const anim = useRef<{ dir: string; warm: boolean }>({ dir: 'fwd', warm: false })
  if (prevKey.current !== key) {
    const from = TABS.findIndex(t => t.path === prevKey.current), to = TABS.findIndex(t => t.path === key)
    scrolls.current.set(prevKey.current, window.scrollY)
    anim.current = { dir: from >= 0 && to >= 0 ? (to > from ? 'right' : 'left') : navDir(), warm: seen.current.has(key) }
    seen.current.add(prevKey.current)
    prevKey.current = key
  }
  useLayoutEffect(() => { window.scrollTo(0, anim.current.dir === 'back' || tab ? scrolls.current.get(key) || 0 : 0) }, [key])
  useEffect(() => { setBack(route.length > 1 || !tab ? () => back('') : null); return () => setBack(null) }, [route.join('/')])

  const start = async () => {
    if (home.data?.round) { go('round/' + home.data.round.id); return }
    try { const r = await post<{ round: Round }>('/rounds'); emit('home'); go('round/' + r.round.id) } catch (e) { toast(humanError(e)) }
  }
  const actions = [
    { id: 'go', label: 'Выбрать вместе', icon: 'cal', run: start },
    { id: 'dice', label: 'Удиви нас', icon: 'dice', run: () => setSurprise('random') },
    { id: 'add', label: 'Добавить идею', icon: 'plus', run: () => setAdding(true) },
    { id: 'ask', label: 'Спросить', icon: 'spark', run: () => go('assistant') },
  ]

  let screen
  switch (top) {
    case 'ideas': screen = <Ideas home={home.data} folder={route[1]} />; break
    case 'triage': screen = <Triage partner={home.data?.partner} />; break
    case 'idea': screen = <IdeaScreen id={Number(route[1])} me={home.data?.me} partner={home.data?.partner} />; break
    case 'round': screen = <RoundScreen id={Number(route[1])} home={home.data} />; break
    case 'calendar': screen = <CalendarScreen home={home.data} />; break
    case 'assistant': screen = <Assistant home={home.data} />; break
    case 'us': screen = <Us home={home.data} />; break
    default: screen = <Home home={home.data} onAdd={() => setAdding(true)} onSurprise={m => setSurprise(m)} onStart={start} />
  }
  const hideIsland = top === 'round' || top === 'idea'
  return (
    <div className="shell">
      {/* the island lives outside the screen: it stays put (and keeps its state) while screens change under it */}
      {!hideIsland && <Island home={home.data} actions={actions} />}
      <main key={key} className={'screen' + (hideIsland ? ' screen--sub' : '') + (anim.current.warm ? ' screen--warm' : '')} data-dir={anim.current.dir}>
        <Suspense fallback={<div style={{ minHeight: '60vh' }} />}>{screen}</Suspense>
      </main>
      {!hideIsland && <PillNav current={tab ? top : '-'} badge={home.data?.counts.unmarked} onPlus={() => setAdding(true)} />}
      <AddSheet open={adding} onClose={() => setAdding(false)} onAdded={() => { emit('ideas'); emit('home') }} />
      {surprise && <Suspense fallback={null}><SurpriseSheet mode={surprise} onClose={() => setSurprise('')} /></Suspense>}
    </div>
  )
}

async function get_me_theme() {
  try { const m = await get<{ theme?: string }>('/me'); setThemePref(m.theme || '') } catch { /* default */ }
}
