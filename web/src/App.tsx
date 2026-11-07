import { useEffect, useState } from 'react'
import { useRoute, go, navDir, back } from './lib/router'
import { scheme, onTheme, setBack } from './lib/tg'
import { useLoad, emit, useOn } from './lib/data'
import { get, post, humanError } from './lib/api'
import type { Home as HomeT, Round } from './lib/types'
import { Burst, Toasts, toast } from './ui/kit'
import { PillNav, Island, TABS } from './ui/Nav'
import { Entry } from './screens/Entry'
import { Home } from './screens/Home'
import { Ideas } from './screens/Ideas'
import { IdeaScreen } from './screens/Idea'
import { RoundScreen } from './screens/Round'
import { Assistant } from './screens/Assistant'
import { CalendarScreen } from './screens/Calendar'
import { Us } from './screens/Us'
import { AddSheet } from './screens/AddSheet'
import { SurpriseSheet } from './screens/Surprise'

let themePref = ''
export const setThemePref = (t: string) => { themePref = t; applyTheme() }
function applyTheme() {
  const s = themePref === 'light' || themePref === 'dark' ? themePref : scheme()
  document.documentElement.dataset.theme = s
  document.querySelector('meta[name=theme-color]')?.setAttribute('content', s === 'dark' ? '#0f0f11' : '#f4f4f4')
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
  useEffect(() => { get_me_theme() }, [])

  const top = route[0]
  const tab = TABS.find(t => t.path === top)
  const isTab = !!tab && (route.length === 1 || top === 'ideas')
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
      <main key={route.join('/')} className="screen" data-dir={navDir()}>
        {!hideIsland && <Island home={home.data} actions={actions} />}
        {screen}
      </main>
      <PillNav current={isTab ? top : tab ? top : ''} badge={home.data?.counts.unmarked} onPlus={() => setAdding(true)} />
      <AddSheet open={adding} onClose={() => setAdding(false)} onAdded={() => { emit('ideas'); emit('home') }} />
      <SurpriseSheet mode={surprise} onClose={() => setSurprise('')} />
    </div>
  )
}

async function get_me_theme() {
  try { const m = await get<{ theme?: string }>('/me'); setThemePref(m.theme || '') } catch { /* default */ }
}
