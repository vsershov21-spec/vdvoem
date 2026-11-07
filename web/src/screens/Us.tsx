import { useState } from 'react'
import type { Home, Round, Settings } from '../lib/types'
import { useLoad, emit } from '../lib/data'
import { patch, post, humanError } from '../lib/api'
import { go } from '../lib/router'
import { money } from '../lib/format'
import { shareLink } from '../lib/tg'
import { Icon } from '../ui/icons'
import { Avatar, Btn, Cover, Empty, Segmented, Sheet, Skel, toast } from '../ui/kit'

const WD = ['воскресенье', 'понедельник', 'вторник', 'среду', 'четверг', 'пятницу', 'субботу']
const MODES: [Settings['suggest_mode'], string, string][] = [
  ['mix', 'Через неделю', 'то лучшее по оценкам, то случайное'],
  ['best', 'По оценке', 'то, что вы оба отметили «хочу»'],
  ['random', 'Рандом', 'случайно, но «очень хотим» выпадает чаще'],
  ['off', 'Не присылать', 'только когда попросите'],
]

export function Us({ home }: { home: Home | null }) {
  const hist = useLoad<{ meetings: Round[] }>('/history')
  const st = useLoad<Settings>('/settings')
  const [sheet, setSheet] = useState<'' | 'budget' | 'plan' | 'suggest'>('')
  if (!home) return <><Skel h={120} /><Skel h={200} /></>
  const save = async (p: Partial<Settings>) => {
    try { const s = await patch<Settings>('/settings', p); st.set(s); toast('Сохранил') } catch (e) { toast(humanError(e)) }
  }
  const s = st.data
  const invite = async () => { try { const { link } = await post<{ link: string }>('/invite'); shareLink(link, 'Давай планировать выходные вместе — в «Вдвоём»') } catch (e) { toast(humanError(e)) } }
  return (
    <>
      <header className="hdr"><h1 className="display">Мы</h1></header>
      <div className="card row rise" style={{ gap: 14 }}>
        <div className="pairava"><Avatar p={home.me} /><Avatar p={home.partner} ghost={!home.partner} /></div>
        <div style={{ flex: 1 }}><b className="headline">{home.me.name}{home.partner ? ' и ' + home.partner.name : ''}</b>
          <div className="caption muted">{home.partner ? `${hist.data?.meetings.length ?? 0} ${['встреч', 'встреча', 'встречи'][pl(hist.data?.meetings.length ?? 0)]} вместе` : 'Партнёр ещё не присоединился'}</div></div>
        {!home.partner && <Btn sm kind="accent" icon="share" onClick={invite}>Пригласить</Btn>}
      </div>

      <div className="sec"><h2 className="title">Где мы были</h2></div>
      {!hist.data ? <Skel h={90} /> : hist.data.meetings.length === 0 ? <div className="card"><Empty title="Пока пусто" text="Здесь появятся ваши встречи — с фото и отметкой, хочется ли повторить." /></div> :
        <div className="rail">{hist.data.meetings.map((r, k) => r.chosen && (
          <div key={r.id} className="icard" style={{ width: 200, ['--d' as any]: k }} onClick={() => go('round/' + r.id)}>
            <Cover idea={r.chosen} h={120}><span>{r.again === 'yes' && <span className="pill pill--glass"><Icon n="heart" s={12} w={2.4} />повторим</span>}</span><div className="cover__title" style={{ fontSize: 15 }}>{r.chosen.title}</div></Cover>
            <span className="caption muted" style={{ padding: '0 6px' }}>{r.when}</span>
          </div>))}</div>}

      <div className="sec"><h2 className="title">Настройки</h2></div>
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
    </>
  )
}

const pl = (n: number) => { const a = n % 100, b = n % 10; return a > 10 && a < 20 ? 0 : b === 1 ? 1 : b > 1 && b < 5 ? 2 : 0 }

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
