export function StatusBadge({ ok }: { ok: boolean }) {
  const cls = ok
    ? 'bg-green-100 text-green-800'
    : 'bg-red-100 text-red-800'
  return (
    <span className={`inline-block rounded px-2 py-0.5 text-xs font-semibold ${cls}`}>
      {ok ? 'OK' : 'FAIL'}
    </span>
  )
}
