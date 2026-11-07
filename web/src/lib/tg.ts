// Thin wrapper over Telegram.WebApp; everything degrades gracefully in a plain browser.
type TGWebApp = any
const w = window as any
// telegram-web-app.js also loads in a plain browser; only a non-empty initData means we are inside Telegram
export const tg: TGWebApp | undefined = w.Telegram?.WebApp?.initData ? w.Telegram.WebApp : undefined

export const initData = (): string => tg?.initData || ''

export function ready() {
  if (!tg) return
  tg.ready()
  tg.expand()
  try { tg.disableVerticalSwipes?.() } catch { /* old clients */ }
  try { tg.setHeaderColor?.('bg_color'); tg.setBackgroundColor?.('bg_color') } catch { /* */ }
  // phones: fullscreen like a native app (Telegram 8.0+); its buttons float on top, --tg-content-safe-area-inset-top keeps content clear
  try { if (/^(ios|android)$/.test(tg.platform) && tg.isVersionAtLeast?.('8.0') && !tg.isFullscreen) tg.requestFullscreen?.() } catch { /* old clients */ }
}

// fullscreen status bar text must contrast with the page
export function statusBar(dark: boolean) {
  try { if (tg?.isVersionAtLeast?.('8.0')) { tg.setHeaderColor?.(dark ? '#0f0f11' : '#f4f4f4'); tg.setBackgroundColor?.(dark ? '#0f0f11' : '#f4f4f4'); tg.setBottomBarColor?.(dark ? '#0f0f11' : '#f4f4f4') } } catch { /* */ }
}

export function scheme(): 'light' | 'dark' {
  if (tg?.colorScheme) return tg.colorScheme
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function onTheme(cb: () => void) {
  tg?.onEvent?.('themeChanged', cb)
  window.matchMedia?.('(prefers-color-scheme: dark)').addEventListener?.('change', cb)
}

export const haptic = {
  tap: () => tg?.HapticFeedback?.impactOccurred?.('light'),
  soft: () => tg?.HapticFeedback?.impactOccurred?.('soft'),
  heavy: () => tg?.HapticFeedback?.impactOccurred?.('medium'),
  select: () => tg?.HapticFeedback?.selectionChanged?.(),
  ok: () => tg?.HapticFeedback?.notificationOccurred?.('success'),
  warn: () => tg?.HapticFeedback?.notificationOccurred?.('warning'),
  err: () => tg?.HapticFeedback?.notificationOccurred?.('error'),
}

let backHandler: (() => void) | null = null
export function setBack(fn: (() => void) | null) {
  if (!tg?.BackButton) return
  if (backHandler) tg.BackButton.offClick(backHandler)
  backHandler = fn
  if (fn) { tg.BackButton.onClick(fn); tg.BackButton.show() } else tg.BackButton.hide()
}

export function openLink(url: string) {
  if (tg?.openLink) tg.openLink(url, { try_instant_view: false })
  else window.open(url, '_blank', 'noopener')
}

export function shareLink(url: string, text: string) {
  const share = `https://t.me/share/url?url=${encodeURIComponent(url)}&text=${encodeURIComponent(text)}`
  if (tg?.openTelegramLink) tg.openTelegramLink(share)
  else window.open(share, '_blank', 'noopener')
}

export function download(url: string, name: string) {
  if (tg?.downloadFile) { try { tg.downloadFile({ url, file_name: name }); return } catch { /* fallback */ } }
  window.open(url, '_blank', 'noopener')
}

export const startParam = (): string => tg?.initDataUnsafe?.start_param || ''

// Face ID / fingerprint through Telegram. Desktop clients expose BiometricManager but have no sensor
// (isBiometricAvailable=false) — the lock is a per-device extra and never applies there.
type Bio = { available: boolean; granted: boolean; type: string }
let bioInit: Promise<Bio> | null = null
export function biometric(): Promise<Bio> {
  const bm = tg?.BiometricManager
  if (!bm || !tg?.isVersionAtLeast?.('7.2')) return Promise.resolve({ available: false, granted: false, type: '' })
  if (!bioInit) bioInit = new Promise<Bio>(res => {
    const done = () => res({ available: !!bm.isBiometricAvailable, granted: !!bm.isAccessGranted, type: bm.biometricType || '' })
    const t = setTimeout(() => res({ available: false, granted: false, type: '' }), 2500) // a client that never answers
    try { if (bm.isInited) { clearTimeout(t); done() } else bm.init(() => { clearTimeout(t); done() }) } catch { clearTimeout(t); res({ available: false, granted: false, type: '' }) }
  })
  return bioInit
}
export const bioRequest = (reason: string) => new Promise<boolean>(res => { try { tg.BiometricManager.requestAccess({ reason }, (g: boolean) => res(!!g)) } catch { res(false) } })
export const bioAuth = (reason: string) => new Promise<boolean>(res => { try { tg.BiometricManager.authenticate({ reason }, (ok: boolean) => res(!!ok)) } catch { res(false) } })
export const bioName = (t: string) => t === 'finger' ? 'отпечаток' : 'Face ID'
