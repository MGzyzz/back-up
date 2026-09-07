export function StatusBadge({ ok }: { ok: boolean }) {
  const cls = ok
    ? 'bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300'
    // Насыщеннее фона строки: у провала строка красная целиком, и бейдж
    // на red-100 сливался бы с ней в одно пятно.
    : 'bg-red-200 text-red-900 dark:bg-red-900 dark:text-red-100'
  return (
    <span className={`inline-block rounded px-2 py-0.5 text-xs font-semibold ${cls}`}>
      {ok ? 'OK' : 'FAIL'}
    </span>
  )
}
