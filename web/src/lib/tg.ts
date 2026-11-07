// Thin wrapper over Telegram.WebApp; everything degrades gracefully in a plain browser.
type TGWebApp = any
const w = window as any
export const tg: TGWebApp | undefined = w.Telegram?.WebApp?.initData !== undefined ? w.Telegram.WebApp : undefined

export const initData = (): string => tg?.initData || ''

export function ready() {
  if (!tg) return
  tg.ready()
  tg.expand()
  try { tg.disableVerticalSwipes?.() } catch { /* old clients */ }
  try { tg.setHeaderColor?.('bg_color'); tg.setBackgroundColor?.('bg_color') } catch { /* */ }
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
