import { useEffect, useState } from 'react'
import { useRoute, go, navDir, back } from './lib/router'
import { scheme, onTheme, setBack, haptic } from './lib/tg'
import { useLoad, emit, useOn } from './lib/data'
import type { Home as HomeT } from './lib/types'
import { Icon } from './ui/icons'
import { Burst, Toasts } from './ui/kit'
import { Home } from './screens/Home'
import { Ideas } from './screens/Ideas'
import { IdeaScreen } from './screens/Idea'
import { RoundScreen } from './screens/Round'
import { Assistant } from './screens/Assistant'
import { Us } from './screens/Us'
import { AddSheet } from './screens/AddSheet'

const TABS: { path: string; icon: string; label: string }[] = [
  { path: '', icon: 'home', label: 'Главная' },
  { path: 'ideas', icon: 'jar', label: 'Копилка' },
  { path: 'assistant', icon: 'spark', label: 'Помощник' },
  { path: 'us', icon: 'us', label: 'Мы' },
]

export function App() {
  const route = useRoute()
  const [theme, setTheme] = useState(scheme())
  const [adding, setAdding] = useState(false)
  const home = useLoad<HomeT>('/home', 20000)
  useOn('home', home.refresh)

  useEffect(() => {
    const apply = () => { const s = scheme(); setTheme(s); document.documentElement.dataset.theme = s
      document.querySelector('meta[name=theme-color]')?.setAttribute('content', s === 'dark' ? '#0f0f11' : '#f4f4f4') }
    apply(); onTheme(apply)
  }, [])

  const top = route[0]
  const tabIdx = TABS.findIndex(t => t.path === top)
  const isTab = tabIdx >= 0 && route.length === 1
  useEffect(() => { setBack(isTab ? null : () => back('')); return () => setBack(null) }, [isTab, route.join('/')])

  let screen
  switch (top) {
    case 'ideas': screen = <Ideas home={home.data} />; break
    case 'idea': screen = <IdeaScreen id={Number(route[1])} me={home.data?.me} />; break
    case 'round': screen = <RoundScreen id={Number(route[1])} home={home.data} />; break
    case 'assistant': screen = <Assistant home={home.data} />; break
    case 'us': case 'settings': case 'history': screen = <Us home={home.data} />; break
    default: screen = <Home home={home.data} onAdd={() => setAdding(true)} />
  }

  return (
    <div className="shell" data-theme={theme}>
      <main key={route.join('/')} className="screen" data-dir={navDir()}>
        {screen}
      </main>
      <nav className="dock" aria-label="Разделы">
        <span className="dock__pill" style={{ ['--i' as any]: Math.max(0, tabIdx), opacity: tabIdx < 0 ? 0 : 1 }} />
        {TABS.map(t => (
          <button key={t.icon} id={'tab-' + t.icon} className="dock__tab" aria-label={t.label} aria-current={t.path === top ? 'page' : undefined}
            onClick={() => { haptic.select(); if (t.path !== top || !isTab) go(t.path) }}>
            <Icon n={t.icon} s={24} w={t.path === top ? 2.1 : 1.8} />
            {t.icon === 'jar' && !!home.data?.counts.unmarked && <span className="dock__badge">{home.data.counts.unmarked}</span>}
          </button>
        ))}
        <button className="dock__plus" aria-label="Добавить идею" onClick={() => { haptic.heavy(); setAdding(true) }}><Icon n="plus" s={26} w={2.4} /></button>
      </nav>
      <AddSheet open={adding} onClose={() => setAdding(false)} onAdded={() => { emit('ideas'); emit('home') }} />
      <Burst />
      <Toasts />
    </div>
  )
}
