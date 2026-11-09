// The mark: two halves of a bridge, his and hers, meeting at the top.
export function Mark({ size = 28, dark }: { size?: number; dark?: boolean }) {
  return (
    <svg width={size} height={size * 56 / 84} viewBox="0 0 84 56" aria-hidden="true">
      <path d="M8 46 Q 22 10 42 10" fill="none" stroke="#ef6537" strokeWidth="9" strokeLinecap="round" />
      <path d="M76 46 Q 62 10 42 10" fill="none" stroke={dark ? '#6b95ff' : 'var(--person-b, #3d6ef5)'} strokeWidth="9" strokeLinecap="round" />
    </svg>
  )
}

// Loading: the halves draw themselves, meet, let go, draw again
export function BridgeLoader({ label = 'Загружаю' }: { label?: string }) {
  return (
    <div className="bloader" role="status" aria-label={label}>
      <svg viewBox="0 0 84 56" aria-hidden="true">
        <path pathLength={1} className="bloader__l" d="M8 46 Q 22 10 42 10" />
        <path pathLength={1} className="bloader__r" d="M76 46 Q 62 10 42 10" />
      </svg>
    </div>
  )
}
