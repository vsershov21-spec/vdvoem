import { useEffect, useState } from 'react'
import type { Home, Idea, Round, Settings, Stats } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { patch, post, del, humanError, ApiError } from '../lib/api'
import { go } from '../lib/router'
import { money, plural } from '../lib/format'
import { useOnline } from '../lib/live'
import { shareLink, haptic, biometric, bioRequest, bioAuth, bioName } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Avatar, Btn, Cover, Empty, Segmented, Sheet, Skel, toast } from '../ui/kit'
import { Heart } from '../ui/Heart'
import { Gauge } from '../ui/Charts'
import { b64, unb64 } from './Entry'
import { setThemePref } from '../App'

const WD = ['воскресенье', 'понедельник', 'вторник', 'среду', 'четверг', 'пятницу', 'субботу']
const MODES: [Settings['suggest_mode'], string, string][] = [
  ['mix', 'Через неделю', 'то лучшее по оценкам, то случайное'],
  ['best', 'По оценке', 'то, что вы оба отметили «хочу»'],
  ['random', 'Рандом', 'случайно, но «очень хотим» выпадает чаще'],
  ['off', 'Не присылать', 'только когда попросите'],
]

type Sec = { login?: string; biometric_lock: boolean; theme: string; passkeys: { id: string; name: string; created_at: number }[]; sessions: { id: string; method: string; device: string; seen_at: number; current: boolean }[] }

export function Us({ home }: { home: Home | null }) {
  const hist = useLoad<{ meetings: Round[] }>('/history')
  const partnerHere = useOnline(home?.partner?.color)
  const st = useLoad<Settings>('/settings')
  const stats = useLoad<Stats>('/stats')
  const ideas = useLoad<{ ideas: Idea[] }>('/ideas?kind=leisure')
  const [sheet, setSheet] = useState<'' | 'budget' | 'plan' | 'suggest' | 'password'>('')
  if (!home) return <><Skel h={120} /><Skel h={200} /></>
  const save = async (p: Partial<Settings>) => {
    try { const s = await patch<Settings>('/settings', p); st.set(s); toast('Сохранил') } catch (e) { toast(humanError(e)) }
  }
  const s = st.data
  const invite = async () => { try { const { link } = await post<{ link: string }>('/invite'); shareLink(link, 'Давай планировать выходные вместе — в «Вдвоём»') } catch (e) { toast(humanError(e)) } }
  const liked = (ideas.data?.ideas || []).filter(i => i.liked_before)
  const n = stats.data?.meetings ?? 0
  return (
    <>
      <header className="hdr"><h1 className="display">Мы</h1></header>
      <div className="card rise" style={{ display: 'grid', gap: 14 }}>
        <div className="row" style={{ gap: 14 }}>
          <div className="pairava"><Avatar p={home.me} /><Avatar p={home.partner} ghost={!home.partner} online={partnerHere} /></div>
          <div style={{ flex: 1 }}><b className="headline">{home.me.name}{home.partner ? ' и ' + home.partner.name : ''}</b>
            <div className="caption muted">{home.partner ? `${n} ${plural(n, 'встреча', 'встречи', 'встреч')} вместе` : 'Партнёр ещё не присоединился'}</div></div>
          {!home.partner && <Btn sm kind="accent" icon="share" onClick={invite}>Пригласить</Btn>}
        </div>
        {stats.data && home.partner && (
          <div className="row" style={{ gap: 12, background: 'var(--surface-soft)', borderRadius: 18, padding: 12 }}>
            <Gauge value={stats.data.match} tone="pink" size={70} />
            <div style={{ flex: 1 }}><b className="title" style={{ color: '#ff3b71' }}>{stats.data.match}%</b><div className="caption muted">совпадение вкусов · {stats.data.both_want} из {stats.data.both_marked} {plural(stats.data.both_marked, 'идеи', 'идей', 'идей')} хотите оба</div></div>
          </div>
        )}
      </div>

      <div className="sec"><h2 className="title"><span className="orange-ind" />Где мы были</h2></div>
      {!hist.data ? <Skel h={90} /> : hist.data.meetings.length === 0 ? <div className="card"><Empty title="Пока пусто" text="Здесь появятся ваши встречи — с фото и отметкой, хочется ли повторить." /></div> :
        <div className="rail">{hist.data.meetings.map((r, k) => r.chosen && (
          <div key={r.id} className="icard" style={{ width: 200, ['--d' as any]: k }} onClick={() => go('round/' + r.id)}>
            <Cover idea={r.chosen} h={120}><span>{r.again === 'yes' && <span className="pill pill--glass"><Icon n="heart" s={12} w={2.4} />повторим</span>}{!!r.photos && <span className="pill pill--glass"><Icon n="photo" s={12} w={2.2} />{r.photos}</span>}</span><div className="cover__title" style={{ fontSize: 15 }}>{r.chosen.title}</div></Cover>
            <span className="caption muted" style={{ padding: '0 6px' }}>{r.when}</span>
          </div>))}</div>}

      {liked.length > 0 && <>
        <div className="sec"><h2 className="title"><span className="orange-ind" />Понравилось</h2><button className="link" onClick={() => go('ideas/liked')}>Все</button></div>
        <div className="list">{liked.slice(0, 5).map(i => <LikedRow key={i.id} i={i} />)}</div>
      </>}

      <TasteSection />

      <div className="sec"><h2 className="title"><span className="orange-ind" />Настройки</h2></div>
      {!s ? <Skel h={180} /> : (
        <div className="list rise">
          <button className="li" onClick={() => setSheet('suggest')}><span className="li__ico"><Icon n="dice" s={19} /></span>
            <span className="li__main"><b>Бот сам предлагает идеи</b><span>{MODES.find(m => m[0] === s.suggest_mode)?.[1]}{s.suggest_mode !== 'off' ? `, в ${WD[s.suggest_weekday]} в ${s.suggest_hour}:00` : ''}</span></span><Icon n="chev" s={18} /></button>
          <button className="li" onClick={() => setSheet('plan')}><span className="li__ico"><Icon n="cal" s={19} /></span>
            <span className="li__main"><b>День планирования</b><span>В {WD[s.plan_weekday]} в {s.plan_hour}:00 спрошу, когда вам удобно</span></span><Icon n="chev" s={18} /></button>
          <button className="li" onClick={() => setSheet('budget')}><span className="li__ico"><Icon n="wallet" s={19} /></span>
            <span className="li__main"><b>Бюджет на встречу</b><span>{s.budget ? 'до ' + money(s.budget) + ' на двоих' : 'без лимита'}</span></span><Icon n="chev" s={18} /></button>
        </div>
      )}
      <Security openPassword={() => setSheet('password')} />
      <p className="caption muted" style={{ margin: '14px 4px' }}>Ночью (23:00–9:00) я молчу — сообщения подождут до утра. Оценки и голоса друг друга вы видите только после своих.</p>

      <Sheet open={sheet === 'suggest'} onClose={() => setSheet('')}>
        {s && <div className="col">
          <h2 className="title">Предложения от бота</h2>
          <p className="callout muted" style={{ margin: 0 }}>Раз в неделю пришлю вам обоим одну идею из копилки — с кнопками «Давай!», «Другую» и «🎲».</p>
          <div className="list">{MODES.map(([m, l, d]) => (
            <button key={m} className="li" onClick={() => save({ suggest_mode: m })}>
              <span className="li__main"><b>{l}</b><span>{d}</span></span>{s.suggest_mode === m && <Icon n="check" s={20} w={2.6} style={{ color: 'var(--accent)' }} />}
            </button>))}</div>
          {s.suggest_mode !== 'off' && <DayHour wd={s.suggest_weekday} h={s.suggest_hour} onChange={(wd, h) => save({ suggest_weekday: wd, suggest_hour: h })} />}
        </div>}
      </Sheet>
      <Sheet open={sheet === 'plan'} onClose={() => setSheet('')}>
        {s && <div className="col"><h2 className="title">День планирования</h2>
          <p className="callout muted" style={{ margin: 0 }}>В этот день я сам начну выбор: спрошу обоих, когда удобно на выходных.</p>
          <DayHour wd={s.plan_weekday} h={s.plan_hour} onChange={(wd, h) => save({ plan_weekday: wd, plan_hour: h })} /></div>}
      </Sheet>
      <Sheet open={sheet === 'budget'} onClose={() => setSheet('')}>
        {s && <Budget value={s.budget} onSave={b => { save({ budget: b }); setSheet(''); emit('home') }} />}
      </Sheet>
      <Sheet open={sheet === 'password'} onClose={() => setSheet('')}>
        <PasswordForm onDone={() => { setSheet(''); emit('security') }} />
      </Sheet>
    </>
  )
}

function LikedRow({ i }: { i: Idea }) {
  const [on, setOn] = useState(false)
  useEffect(() => { const t = setTimeout(() => setOn(true), 250); return () => clearTimeout(t) }, [])
  return (
    <button className="li" onClick={() => go('idea/' + i.id)}>
      <span className="li__ico" style={{ background: 'var(--accent-soft)' }}><Heart on={on} size={26} stroke={2} /></span>
      <span className="li__main"><b>{i.title}</b><span>{i.meta}</span></span><Icon n="chev" s={18} />
    </button>
  )
}

function Security({ openPassword }: { openPassword: () => void }) {
  const sec = useLoad<Sec>('/security')
  const [busy, setBusy] = useState(false)
  useEffect(() => { const f = () => sec.refresh(); addEventListener('vd-security', f); return () => removeEventListener('vd-security', f) }, [sec.refresh])
  const s = sec.data
  const [bio, setBio] = useState<{ available: boolean; type: string } | null>(null)
  useEffect(() => { biometric().then(setBio) }, [])
  const setPrefs = async (p: object) => { try { sec.set(await patch<Sec>('/security/prefs', p)) } catch (e) { toast(humanError(e)) } }
  const toggleLock = async () => {
    if (!s) return
    if (!s.biometric_lock && bio?.available) {
      if (!(await bioRequest('Защищать «Вдвоём» — ' + bioName(bio.type)))) { toast('Доступ к ' + bioName(bio.type) + ' не дан'); return }
      if (!(await bioAuth('Проверим, что всё работает'))) { toast('Не получилось подтвердить — защита не включена'); return }
    }
    setPrefs({ biometric_lock: !s.biometric_lock })
  }
  const addPasskey = async () => {
    if (!window.PublicKeyCredential) { toast('Этот браузер не поддерживает Face ID для сайтов'); return }
    setBusy(true)
    try {
      const b = await post<any>('/security/passkey/begin')
      const cred = await navigator.credentials.create({ publicKey: {
        challenge: unb64(b.challenge), rp: b.rp, user: { id: unb64(b.user.id), name: b.user.name, displayName: b.user.display_name },
        pubKeyCredParams: [{ type: 'public-key', alg: -7 }, { type: 'public-key', alg: -257 }],
        authenticatorSelection: { residentKey: 'required', userVerification: 'required' }, attestation: 'none', timeout: b.timeout,
        excludeCredentials: (b.exclude as string[]).map(id => ({ type: 'public-key' as const, id: unb64(id) })),
      } }) as PublicKeyCredential | null
      if (!cred) throw new Error('cancel')
      const r = cred.response as AuthenticatorAttestationResponse
      const spki = r.getPublicKey()
      if (!spki) throw new Error('nokey')
      const name = /iPhone|iPad|Mac/.test(navigator.userAgent) ? 'Face ID · ' + (/Mac/.test(navigator.userAgent) ? 'Mac' : 'iPhone') : 'Passkey · ' + (navigator.platform || 'устройство')
      sec.set(await post<Sec>('/security/passkey/finish', { id: cred.id, challenge: b.challenge, client_data_json: b64(r.clientDataJSON), authenticator_data: b64(r.getAuthenticatorData()),
        public_key: b64(spki), alg: r.getPublicKeyAlgorithm(), name }))
      haptic.ok(); toast('Face ID добавлен — теперь можно входить в браузере')
    } catch (e) { toast(e instanceof ApiError ? humanError(e) : 'Не получилось добавить Face ID') } finally { setBusy(false) }
  }
  const logoutHere = async () => { await post('/auth/logout'); location.href = '/app/' }
  if (!s) return <Skel h={160} />
  return (<>
    <div className="sec"><h2 className="title"><span className="orange-ind" />Вход и безопасность</h2></div>
    <div className="list rise">
      <div className="li" style={{ cursor: 'default' }}><span className="li__ico"><Icon n="sun" s={19} /></span><span className="li__main"><b>Тема</b></span></div>
      <div style={{ padding: '0 16px 14px' }}><Segmented<string> items={[['', 'Авто'], ['light', 'Светлая'], ['dark', 'Тёмная']]} value={s.theme} onChange={v => { setThemePref(v); setPrefs({ theme: v }) }} /></div>
      {(bio?.available || s.biometric_lock) && <button className="li" onClick={toggleLock}><span className="li__ico"><Icon n="face" s={19} /></span>
        <span className="li__main"><b>Защита {bio ? bioName(bio.type) : 'Face ID'}</b><span>Спрашивать при открытии на телефоне. На компьютере не спрашивает</span></span>
        <span className="toggle" data-on={s.biometric_lock || undefined} /></button>}
      <button className="li" onClick={addPasskey} disabled={busy}><span className="li__ico"><Icon n="face" s={19} /></span>
        <span className="li__main"><b>Face ID для браузера</b><span>{s.passkeys.length ? s.passkeys.map(p => p.name).join(', ') : 'Вход на сайте без пароля'}</span></span>
        <span className="pill">{busy ? '…' : 'Добавить'}</span></button>
      {s.passkeys.map(p => (
        <div key={p.id} className="li" style={{ cursor: 'default', paddingLeft: 62 }}><span className="li__main"><b style={{ fontSize: 14 }}>{p.name}</b><span>добавлен {new Date(p.created_at * 1000).toLocaleDateString('ru')}</span></span>
          <button className="btn btn--ghost btn--sm" onClick={async () => sec.set(await del<Sec>('/security/passkeys/' + encodeURIComponent(p.id)))}>Удалить</button></div>
      ))}
      <button className="li" onClick={openPassword}><span className="li__ico"><Icon n="lock" s={19} /></span>
        <span className="li__main"><b>Логин и пароль</b><span>{s.login ? 'логин: ' + s.login : 'не задан'}</span></span><Icon n="chev" s={18} /></button>
      {s.sessions.length > 0 && <>
        {s.sessions.map(x => (
          <div key={x.id} className="li" style={{ cursor: 'default' }}><span className="li__ico"><Icon n={x.device === 'iPhone' || x.device === 'Android' ? 'user' : 'home'} s={19} /></span>
            <span className="li__main"><b>{x.device}{x.current ? ' · это устройство' : ''}</b><span>{({ telegram: 'через Telegram', password: 'по паролю', passkey: 'по Face ID' } as Record<string, string>)[x.method]} · {new Date(x.seen_at * 1000).toLocaleDateString('ru')}</span></span>
            {x.current ? <button className="btn btn--ghost btn--sm" onClick={logoutHere}>Выйти</button> : <button className="btn btn--ghost btn--sm" onClick={async () => sec.set(await del<Sec>('/security/sessions/' + x.id))}>Завершить</button>}
          </div>
        ))}
        <div style={{ padding: 12 }}><Btn kind="danger" block sm onClick={async () => { await post('/security/logout-all'); sec.refresh(); toast('Все браузерные сессии завершены') }}>Выйти на всех браузерах</Btn></div>
      </>}
    </div>
  </>)
}

function PasswordForm({ onDone }: { onDone: () => void }) {
  const [login, setLogin] = useState(''), [p1, setP1] = useState(''), [p2, setP2] = useState(''), [err, setErr] = useState('')
  const save = async () => {
    setErr('')
    if (p1 !== p2) { setErr('Пароли не совпадают'); return }
    try { await post('/security/password', { login, password: p1 }); toast('Логин и пароль сохранены'); dispatchEvent(new Event('vd-security')); onDone() }
    catch (e) { setErr(e instanceof ApiError ? ({ bad_login_name: 'Логин: 3–32 латинские буквы, цифры, точка, дефис', weak_password: 'Пароль — от 8 символов и не равен логину', login_taken: 'Этот логин уже занят' } as Record<string, string>)[e.code] || humanError(e) : humanError(e)) }
  }
  return (
    <div className="col">
      <h2 className="title">Логин и пароль</h2>
      <p className="callout muted" style={{ margin: 0 }}>Для входа в браузере, если под рукой нет Telegram.</p>
      <div className="field"><label>Логин</label><input className="input" autoComplete="username" value={login} onChange={e => setLogin(e.target.value)} placeholder="например, vova" /></div>
      <div className="field"><label>Пароль</label><input className="input" type="password" autoComplete="new-password" value={p1} onChange={e => setP1(e.target.value)} /></div>
      <div className="field"><label>Ещё раз</label><input className="input" type="password" autoComplete="new-password" value={p2} onChange={e => setP2(e.target.value)} /></div>
      {err && <p className="caption" style={{ color: 'var(--danger)', margin: 0 }} role="alert">{err}</p>}
      <Btn kind="primary" block onClick={save} disabled={!login || !p1}>Сохранить</Btn>
    </div>
  )
}

function DayHour({ wd, h, onChange }: { wd: number; h: number; onChange: (wd: number, h: number) => void }) {
  const days: [string, string][] = [['1', 'Пн'], ['2', 'Вт'], ['3', 'Ср'], ['4', 'Чт'], ['5', 'Пт'], ['6', 'Сб'], ['0', 'Вс']]
  return (<>
    <Segmented<string> items={days} value={String(wd)} onChange={v => onChange(Number(v), h)} />
    <Segmented<string> items={[['10', '10:00'], ['13', '13:00'], ['17', '17:00'], ['19', '19:00'], ['21', '21:00']]} value={String(h)} onChange={v => onChange(wd, Number(v))} />
  </>)
}

function Budget({ value, onSave }: { value: number; onSave: (kop: number) => void }) {
  const [v, setV] = useState(value ? String(Math.round(value / 100)) : '')
  const presets = [2000, 4000, 7000, 12000]
  return (
    <div className="col">
      <h2 className="title">Бюджет на встречу</h2>
      <p className="callout muted" style={{ margin: 0 }}>Идеи дороже я не предложу. Цены беру со страниц мест, примерно.</p>
      <div className="sugg">{presets.map(p => <button key={p} onClick={() => setV(String(p))}>{money(p * 100)}</button>)}<button onClick={() => setV('')}>Без лимита</button></div>
      <input className="input" inputMode="numeric" placeholder="Без лимита" value={v} onChange={e => setV(e.target.value.replace(/\D/g, ''))} />
      <Btn kind="primary" block onClick={() => onSave((Number(v) || 0) * 100)}>Сохранить</Btn>
    </div>
  )
}

// Personal: what the app learned about *me* from my marks, what I told the assistant (mine only),
// and the couple's goals the scout watches for (also months ahead).
type Prof = { taste: { likes: string[]; dislikes: string[]; marks: number }; memories: { id: number; text: string }[] }
type Goals = { goals: { id: number; text: string; mine: boolean }[] }

function TasteSection() {
  const prof = useLoad<Prof>('/profile')
  const goals = useLoad<Goals>('/goals')
  const [text, setText] = useState('')
  const delMem = async (id: number) => { try { await del('/memories/' + id); prof.refresh() } catch (e) { toast(humanError(e)) } }
  const delGoal = async (id: number) => { try { await del('/goals/' + id); goals.refresh() } catch (e) { toast(humanError(e)) } }
  const addGoal = async () => { const t = text.trim(); if (!t) return; setText(''); try { await post('/goals', { text: t }); goals.refresh(); toast('Скаут будет следить') } catch (e) { toast(humanError(e)) } }
  const t = prof.data?.taste
  return (<>
    <div className="sec"><h2 className="title"><span className="orange-ind" />Твой вкус</h2><button className="link" onClick={() => go('taste')}>Настроить</button></div>
    <div className="card col" data-testid="taste">
      <button className="tastecta" onClick={() => go('taste')}>
        <span className="tastecta__e">✨</span>
        <span className="col" style={{ gap: 2, flex: 1, textAlign: 'left' }}><b>Мой вкус</b><span className="caption muted">Что нравится — от сфер до жанров, и как. Подборки сразу точнее</span></span>
        <Icon n="chev" s={16} w={2.4} />
      </button>
      {!t ? <Skel h={60} /> : t.marks < 3
        ? <p className="callout muted" style={{ margin: 0 }}>Отметь несколько идей «хочу» или «пропущу» — и я начну понимать, что тебе нравится. Учусь на каждой отметке, у каждого из вас — свой вкус.</p>
        : <>
          {t.likes.length > 0 && <div><span className="caption muted">Тебе нравится</span><div className="chips">{t.likes.map(x => <span key={x} className="pill">{x}</span>)}</div></div>}
          {t.dislikes.length > 0 && <div><span className="caption muted">Не очень</span><div className="chips">{t.dislikes.map(x => <span key={x} className="pill pill--muted">{x}</span>)}</div></div>}
          <span className="caption muted">По {t.marks} отметкам. Это видишь только ты.</span>
        </>}
      {!!prof.data?.memories.length && <div>
        <span className="caption muted">Помощник помнит о тебе</span>
        <div className="list" style={{ marginTop: 6 }}>{prof.data.memories.map(m => (
          <div key={m.id} className="li"><span className="li__main"><b style={{ fontWeight: 550 }}>{m.text}</b></span>
            <button className="iconbtn" aria-label={'Забыть: ' + m.text} onClick={() => delMem(m.id)}><Icon n="x" s={14} /></button></div>))}</div>
      </div>}
      <span className="caption muted">Скажи помощнику «запомни, что…» — и он будет это учитывать.</span>
    </div>
    <div className="sec"><h2 className="title"><span className="orange-ind" />Скаут следит</h2><button className="link" onClick={() => go('ideas/scout')}>Афиша</button></div>
    <div className="card col" data-testid="goals">
      {(goals.data?.goals || []).map(g => (
        <div key={g.id} className="li"><span className="li__main"><b style={{ fontWeight: 550 }}>🎯 {g.text}</b></span>
          <button className="iconbtn" aria-label={'Убрать цель: ' + g.text} onClick={() => delGoal(g.id)}><Icon n="x" s={14} /></button></div>))}
      <form className="places__add" onSubmit={e => { e.preventDefault(); addGoal() }}>
        <input className="input" placeholder="Театр в январе, концерт Земфиры…" value={text} maxLength={160} onChange={e => setText(e.target.value)} aria-label="Новая цель для скаута" />
        <button className="places__send" type="submit" disabled={!text.trim()} aria-label="Добавить цель"><Icon n="plus" s={18} w={2.6} /></button>
      </form>
      <span className="caption muted">Раз в неделю скаут ищет и на месяцы вперёд — напишет, когда появятся билеты.</span>
    </div>
  </>)
}
