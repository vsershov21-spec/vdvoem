// Telegram's Mini App SDK is served from our own server: from some networks telegram.org answers in seconds,
// and the app could not start until it did (a long grey screen). Fetched at build time; index.html falls back
// to telegram.org only when this copy is missing.
import { writeFile, stat } from 'node:fs/promises'
const out = new URL('../public/telegram-web-app.js', import.meta.url)
try {
  const ctl = AbortSignal.timeout(15000)
  const r = await fetch('https://telegram.org/js/telegram-web-app.js?56', { signal: ctl })
  const js = await r.text()
  if (!r.ok || !js.includes('WebApp')) throw new Error('bad answer ' + r.status)
  await writeFile(out, js)
  console.log('telegram-web-app.js: ' + js.length + ' bytes')
} catch (e) {
  const have = await stat(out).catch(() => null)
  console.warn('telegram-web-app.js not fetched (' + e.message + ')' + (have ? ', keeping the old copy' : ', index.html will use telegram.org'))
  if (process.env.CI && !have) process.exit(1) // a release must carry its own copy
}
