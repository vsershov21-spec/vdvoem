// Entry: the lamp lights itself (lamp_login_animation donor), then either the particle intro (particleAnimation donor)
// hands off to the app, or the glass login card offers Telegram / Face ID / login+password.
import { useEffect, useMemo, useRef, useState } from 'react'
import { motion, useMotionValue, useTransform, AnimatePresence } from 'framer-motion'
import { Particles, type Shape } from '../ui/Particles'
import { Icon } from '../ui/icons'
import { get, post, humanError, ApiError } from '../lib/api'
import { prefetch } from '../lib/data'
import { openLink, haptic, biometric, bioAuth, bioRequest, bioName } from '../lib/tg'
import './entry.css'

type Phase = 'dark' | 'on' | 'intro' | 'login' | 'locked' | 'out'

function Fireflies({ on }: { on: boolean }) {
  const flies = useMemo(() => [...Array(16)].map(() => {
    const x: string[] = [], y: string[] = []
    for (let i = 0; i < 6; i++) { x.push(Math.floor(Math.random() * 100) + 'vw'); y.push(Math.floor(Math.random() * 100) + 'vh') }
    x.push(x[0]); y.push(y[0])
    return { x, y, delay: Math.random() * 3, duration: Math.random() * 30 + 35, size: Math.random() * 4 + 3 }
  }), [])
  if (!on) return null
  return (
    <div className="flies" aria-hidden="true">
      {flies.map((f, i) => (
        <motion.i key={i} initial={{ x: f.x[0], y: f.y[0], opacity: 0 }}
          animate={{ opacity: [0.4, 1, 0.7, 1, 0.5, 1], x: f.x, y: f.y }}
          transition={{ duration: f.duration, repeat: Infinity, delay: f.delay, ease: 'easeInOut', opacity: { duration: 3, repeat: Infinity, ease: 'easeInOut' } }}
          style={{ width: f.size, height: f.size }} />
      ))}
    </div>
  )
}

function Lamp({ on, onPull }: { on: boolean; onPull: () => void }) {
  const x = useMotionValue(0), y = useMotionValue(0)
  const d = useTransform([x, y], ([lx, ly]: number[]) => `M 0 0 L ${lx} ${80 + ly}`)
  // the lamp switches itself on: the cord is pulled once, by itself
  useEffect(() => {
    const t1 = setTimeout(() => { y.set(0); animateTo(y, 34, 180) }, 420)
    const t2 = setTimeout(() => animateTo(y, 0, 380), 640)
    return () => { clearTimeout(t1); clearTimeout(t2) }
  }, [])
  return (
    <div className="lamp" data-on={on || undefined}>
      <div className="lamp__glow" />
      <div className="lamp__head" />
      <div className="lamp__beam" />
      <div className="lamp__stem" />
      <div className="lamp__base" />
      <div className="lamp__desk" />
      <svg className="lamp__cord"><motion.path d={d} stroke="#2a2a2a" strokeWidth="2" strokeLinecap="round" /></svg>
      <motion.div className="lamp__handle" style={{ x, y }} drag dragConstraints={{ top: 0, bottom: 0, left: 0, right: 0 }}
        dragElastic={{ top: 0, bottom: 0.6, left: 0.3, right: 0.3 }} dragTransition={{ bounceStiffness: 300, bounceDamping: 4 }}
        onDragEnd={(_, info) => { if (info.offset.y > 30) { haptic.heavy(); onPull() } }} aria-label="Шнур лампы" role="button" />
    </div>
  )
}

function animateTo(v: ReturnType<typeof useMotionValue<number>>, to: number, ms: number) {
  const from = v.get(), t0 = performance.now()
  const step = (t: number) => { const p = Math.min(1, (t - t0) / ms), e = 1 - Math.pow(1 - p, 3); v.set(from + (to - from) * e); if (p < 1) requestAnimationFrame(step) }
  requestAnimationFrame(step)
}

// e2e and screenshots: ?nointro=1 skips the lamp-and-particles show once signed in
const noIntro = (() => { try { const q = new URLSearchParams(location.search).get('nointro'); if (q) sessionStorage.setItem('vd-nointro', q); return sessionStorage.getItem('vd-nointro') === '1' } catch { return false } })()
// the lamp-and-bridge show is for the very first open only: every later open goes straight in (it cost 5–8 s a day)
const seenToday = () => { try { return !!localStorage.getItem('vd-intro') } catch { return true } }
const markSeen = () => { try { localStorage.setItem('vd-intro', new Date().toDateString()) } catch { /* private mode */ } }

export function Entry({ onEnter }: { onEnter: () => void }) {
  const [phase, setPhase] = useState<Phase>('dark')
  const [netErr, setNetErr] = useState(false)
  const quick = useRef(seenToday()).current
  const [lit, setLit] = useState(false)
  const [shape, setShape] = useState<Shape>('sphere')
  const [dissolve, setDissolve] = useState(false)
  const authed = useRef<null | boolean>(null)
  const lock = useRef(false)
  const timers = useRef<number[]>([])
  const later = (ms: number, f: () => void) => { timers.current.push(window.setTimeout(f, ms)) }

  useEffect(() => {
    // who is it? (inside Telegram initData; in a browser, a session cookie)
    // a network blip must not leave the lamp waiting forever: retry, then offer a button
    let tries = 0
    prefetch(['/home']) // in parallel with «who is it»: the first screen's data is on its way from the first millisecond
    const who = () => get<{ biometric_lock?: boolean }>('/me').then(m => { authed.current = true; lock.current = !!m.biometric_lock; (window as any).__vdlog?.('me', ''); if (tries) prefetch(['/home']) })
      .catch(e => {
        if (e instanceof ApiError && e.status === 401) { authed.current = false; return }
        if (++tries < 4) later(600 * tries, who); else setNetErr(true)
      })
    who()
    // the full show on the first open ever; afterwards nothing waits
    later(noIntro || quick ? 0 : 650, () => { setLit(true); setPhase('on'); haptic.soft() }) // after the first open of the day nothing waits for the lamp
    return () => timers.current.forEach(clearTimeout)
  }, [])

  useEffect(() => {
    if (phase !== 'on') return
    const decide = () => {
      if (authed.current === null) { later(200, decide); return }
      if (!authed.current) { later(450, () => setPhase('login')); return }
      if (lock.current) {
        // the lock only applies where Telegram has a sensor (not on desktop clients)
        biometric().then(b => { if (!b.available) { intro(); return } setBioType(b.type); setPhase('locked'); unlock() })
        return
      }
      intro()
    }
    later(noIntro || quick ? 0 : 500, decide)
  }, [phase])

  const intro = () => {
    if (noIntro) { onEnter(); return }
    setPhase('intro')
    const short = seenToday() || matchMedia('(prefers-reduced-motion: reduce)').matches
    markSeen()
    shortRef.current = short
    // the show is once; every other open goes straight in (the data is already on its way)
    if (short) { finish(); return }
    later(100, () => setShape('bridge'))
    later(9000, finish) // safety net on very slow devices
  }
  const shortRef = useRef(false)
  // the sequence follows the particles: a cloud → the bridge (two arcs meet) → light runs up → dissolve into the app
  const formed = (s: Shape) => {
    if (s === 'bridge') later(shortRef.current ? 700 : 1600, finish) // hold while the light runs up the arcs and meets
  }
  const ended = useRef(false)
  const finish = () => {
    if (ended.current) return
    ended.current = true
    setDissolve(true); setPhase('out')
    later(shortRef.current ? 160 : 650, onEnter)
  }
  const [bioType, setBioType] = useState('')
  const [lockErr, setLockErr] = useState('')
  const [asked, setAsked] = useState(false)
  const unlock = async () => {
    setLockErr('')
    const b = await biometric()
    if (!b.available) { intro(); return }
    if (!b.granted && !(await bioRequest('Открывать «Вдвоём» по ' + (b.type === 'finger' ? 'отпечатку' : 'Face ID')))) { setLockErr('Доступ к ' + bioName(b.type) + ' не дан — подтверди вход в боте'); return }
    if (await bioAuth('Войти во «Вдвоём»')) { haptic.ok(); intro() } else { haptic.err(); setLockErr('Не получилось — попробуй ещё раз или подтверди в боте') }
  }
  // the way past the lock when the sensor fails: the bot asks «Это ты?» in your own chat
  const poll = useRef(0)
  useEffect(() => () => clearInterval(poll.current), [])
  const viaBot = async () => {
    setLockErr('')
    try {
      const r = await post<{ code: string }>('/security/unlock')
      setAsked(true)
      clearInterval(poll.current)
      poll.current = window.setInterval(async () => {
        try {
          const s = await get<{ ok: boolean; alive: boolean }>('/auth/telegram/' + r.code)
          if (s.ok) { clearInterval(poll.current); haptic.ok(); intro() }
          else if (!s.alive) { clearInterval(poll.current); setAsked(false); setLockErr('Запрос устарел — попробуй ещё раз') }
        } catch { /* keep polling */ }
      }, 1500)
    } catch (e) { setLockErr(humanError(e)) }
  }

  return (
    <div className="room" data-on={lit || undefined} onClick={() => { if (phase === 'intro') finish() }}>
      <motion.div className="room__light" initial={false} animate={{ opacity: lit ? 1 : 0, scale: lit ? 1.5 : 0.5 }} transition={{ duration: 0.8, ease: 'easeOut' }} />
      <Fireflies on={lit} />
      <div className="room__lamp"><Lamp on={lit} onPull={() => setLit(v => !v)} /></div>

      <AnimatePresence>
        {phase === 'dark' && <motion.p className="room__hint" exit={{ opacity: 0, y: -12 }}>Включаю свет…</motion.p>}
        {netErr && <motion.div key="net" className="glass login" initial={{ opacity: 0, y: 30 }} animate={{ opacity: 1, y: 0 }}>
          <h1>Нет связи</h1><p>Не получилось достучаться до сервера. Проверь интернет и попробуй ещё раз.</p>
          <button className="lbtn lbtn--light" onClick={() => location.reload()}>Попробовать снова</button></motion.div>}
      </AnimatePresence>

      {(phase === 'intro' || phase === 'out') && (
        <div className="intro">
          <div className="intro__canvas"><Particles from="sphere" shape={shape} dissolve={dissolve} scale={0.3} speed={1.6} onFormed={formed} /></div>
          <motion.div className="intro__word" initial={{ opacity: 0, y: 14, filter: 'blur(8px)' }} animate={dissolve ? { opacity: 0, y: -10 } : { opacity: 1, y: 0, filter: 'blur(0px)' }}
            transition={{ delay: dissolve ? 0 : 1.2, duration: 0.7 }}>
            <b>вдвоём</b><span>{shape === 'bridge' ? 'каждый строит свою половину — встречаемся посередине' : 'двое, одно время, общее «да»'}</span>
            <span className="intro__what">общие идеи, куда сходить · выбираем вместе · ничего не забываем</span>
          </motion.div>
          {!dissolve && <span className="intro__skip">нажми, чтобы пропустить</span>}
        </div>
      )}

      {phase === 'locked' && (
        <motion.div className="glass login" initial={{ opacity: 0, y: 30 }} animate={{ opacity: 1, y: 0 }}>
          <h1>С возвращением</h1>
          <p>Приложение защищено {bioType === 'finger' ? 'отпечатком' : 'Face ID'}</p>
          {asked ? (
            <div className="login__wait">
              <span className="lspin" />
              <b>Подтверди в боте</b>
              <span>Нажми «Да, это я» в чате с ботом — приложение откроется само.</span>
            </div>
          ) : <>
            <button className="lbtn lbtn--light" onClick={unlock}><Icon n="face" s={22} />Разблокировать</button>
            <button className="lbtn lbtn--ghost" onClick={viaBot}><Icon n="plane" s={20} w={2.2} />Подтвердить в боте</button>
          </>}
          {lockErr && <p className="login__err" role="alert">{lockErr}</p>}
        </motion.div>
      )}

      <AnimatePresence>{phase === 'login' && <Login onDone={() => { setPhase('on'); authed.current = true; intro() }} />}</AnimatePresence>
    </div>
  )
}

type Method = 'tg' | 'face' | 'pass'

function Login({ onDone }: { onDone: () => void }) {
  const [m, setM] = useState<Method>('tg')
  const [err, setErr] = useState('')
  const [wait, setWait] = useState<{ code: string; link: string } | null>(null)
  const [login, setLogin] = useState(''), [pass, setPass] = useState('')
  const [busy, setBusy] = useState(false)
  const poll = useRef(0)
  useEffect(() => () => clearInterval(poll.current), [])

  const viaTG = async () => {
    setErr(''); setBusy(true)
    try {
      const r = await post<{ code: string; link: string }>('/auth/telegram')
      setWait(r); openLink(r.link)
      clearInterval(poll.current)
      poll.current = window.setInterval(async () => {
        try {
          const s = await get<{ ok: boolean; alive: boolean }>('/auth/telegram/' + r.code)
          if (s.ok) { clearInterval(poll.current); haptic.ok(); onDone() }
          else if (!s.alive) { clearInterval(poll.current); setWait(null); setErr('Ссылка устарела — попробуй ещё раз') }
        } catch { /* keep polling */ }
      }, 1500)
    } catch (e) { setErr(humanError(e)) } finally { setBusy(false) }
  }
  const viaFace = async () => {
    setErr('')
    if (!window.PublicKeyCredential) { setErr('Этот браузер не поддерживает Face ID для сайтов'); return }
    setBusy(true)
    try {
      const b = await post<{ challenge: string; rp_id: string; timeout: number }>('/auth/passkey/begin')
      const cred = await navigator.credentials.get({ publicKey: { challenge: unb64(b.challenge), rpId: b.rp_id, userVerification: 'required', timeout: b.timeout } }) as PublicKeyCredential | null
      if (!cred) throw new Error('cancel')
      const r = cred.response as AuthenticatorAssertionResponse
      await post('/auth/passkey/finish', { id: cred.id, challenge: b.challenge, client_data_json: b64(r.clientDataJSON), authenticator_data: b64(r.authenticatorData), signature: b64(r.signature) })
      haptic.ok(); onDone()
    } catch (e) {
      setErr(e instanceof ApiError ? 'Этот Face ID не привязан. Войди через Telegram и добавь его в «Мы → Безопасность».' : 'Face ID не подтверждён')
    } finally { setBusy(false) }
  }
  const viaPass = async () => {
    setErr(''); setBusy(true)
    try { await post('/auth/password', { login, password: pass }); haptic.ok(); onDone() }
    catch (e) { haptic.err(); setErr(e instanceof ApiError && e.code === 'locked' ? 'Слишком много попыток — подожди 15 минут' : 'Неверный логин или пароль') }
    finally { setBusy(false) }
  }

  const tabs: [Method, string, string][] = [['tg', 'Telegram', 'plane'], ['face', 'Face ID', 'face'], ['pass', 'Пароль', 'lock']]
  return (
    <motion.div className="glass login" initial={{ opacity: 0, y: 40, filter: 'blur(10px)' }} animate={{ opacity: 1, y: 0, filter: 'blur(0px)' }}
      exit={{ opacity: 0, y: 20 }} transition={{ type: 'spring', stiffness: 110, damping: 18, delay: 0.1 }}>
      <div className="login__shine" />
      <h1>С возвращением</h1>
      <p>Войди, чтобы планировать вдвоём</p>
      <div className="lseg" role="tablist" style={{ ['--i' as any]: tabs.findIndex(t => t[0] === m) }}>
        {tabs.map(([k, l, ic]) => <button key={k} role="tab" aria-selected={m === k} onClick={() => { haptic.select(); setM(k); setErr('') }}><Icon n={ic} s={16} w={2} />{l}</button>)}
      </div>
      <AnimatePresence mode="wait">
        <motion.div key={m} className="login__body" initial={{ opacity: 0, x: 12 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: -12 }} transition={{ duration: 0.18 }}>
          {m === 'tg' && (wait ? (
            <div className="login__wait">
              <span className="lspin" />
              <b>Подтверди вход в боте</b>
              <span>Нажми «Да, это я» в Telegram — эта страница войдёт сама.</span>
              <button className="lbtn lbtn--ghost" onClick={() => openLink(wait.link)}>Открыть бота ещё раз</button>
            </div>
          ) : <>
            <button className="lbtn lbtn--tg" onClick={viaTG} disabled={busy}><Icon n="plane" s={20} w={2.2} />Войти через Telegram</button>
            <span className="login__note">Бот спросит «Это ты?» — один тап, без паролей</span>
          </>)}
          {m === 'face' && <>
            <button className="lbtn lbtn--light" onClick={viaFace} disabled={busy}><Icon n="face" s={22} />Войти с Face ID</button>
            <span className="login__note">Работает, если ты добавил Face ID в «Мы → Безопасность»</span>
          </>}
          {m === 'pass' && <form className="login__form" onSubmit={e => { e.preventDefault(); viaPass() }}>
            <label className="lfield"><Icon n="user" s={19} /><input autoComplete="username" placeholder="Логин" value={login} onChange={e => setLogin(e.target.value)} /></label>
            <label className="lfield"><Icon n="lock" s={19} /><input type="password" autoComplete="current-password" placeholder="Пароль" value={pass} onChange={e => setPass(e.target.value)} /></label>
            <button className="lbtn lbtn--light" type="submit" disabled={busy || !login || !pass}>Войти</button>
          </form>}
        </motion.div>
      </AnimatePresence>
      {err && <p className="login__err" role="alert">{err}</p>}
    </motion.div>
  )
}

export const b64 = (b: ArrayBuffer) => btoa(String.fromCharCode(...new Uint8Array(b))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
export const unb64 = (s: string) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((s.length + 3) % 4)), c => c.charCodeAt(0))
