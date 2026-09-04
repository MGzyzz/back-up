/** Прочерк вместо пустоты — тот же знак, что и в отчёте Google Sheets. */
const NO_VALUE = '—'

/** Время без даты: дата одна на весь экран и стоит в поле выбора. */
export function formatTime(iso: string | null): string {
  if (!iso) return NO_VALUE
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return NO_VALUE
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/**
 * Длительность в том же виде, что и в Google Sheets: "1h 23m".
 * Формат повторяет humanDuration из internal/report — чтобы отчёт и дашборд
 * читались одинаково.
 */
export function formatDuration(seconds: number | null): string {
  if (seconds === null || seconds < 0) return NO_VALUE
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor(seconds / 60) % 60
  return `${hours}h ${String(minutes).padStart(2, '0')}m`
}
