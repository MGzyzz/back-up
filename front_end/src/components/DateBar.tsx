import { todayISO } from '../lib/date'

type Props = {
  date: string
  onDateChange: (date: string) => void
  dateChanged: boolean
  onRefresh: () => void
  busy: boolean
  /** Когда сервер получил эти данные; null, пока данных нет. */
  fetchedAt: string | null
  cached: boolean
  hideSuccessful: boolean
  onHideSuccessfulChange: (hide: boolean) => void
}

export function DateBar({
  date,
  onDateChange,
  dateChanged,
  onRefresh,
  busy,
  fetchedAt,
  cached,
  hideSuccessful,
  onHideSuccessfulChange,
}: Props) {
  const today = todayISO()

  return (
    <form
      className="mb-6 flex flex-wrap items-center gap-3"
      onSubmit={(event) => {
        event.preventDefault()
        if (!date || (busy && !dateChanged)) return
        onRefresh()
      }}
    >
      <input
        type="date"
        aria-label="Дата отчёта"
        value={date}
        max={today}
        onChange={(e) => onDateChange(e.target.value)}
        className="rounded border border-gray-300 bg-white px-3 py-1.5 text-gray-900 [color-scheme:light] dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100 dark:[color-scheme:dark]"
      />
      <button
        type="submit"
        disabled={(busy && !dateChanged) || !date}
        className="min-w-32 cursor-pointer rounded border border-gray-300 px-3 py-1.5 hover:bg-gray-50 disabled:cursor-wait disabled:opacity-70 dark:border-gray-700 dark:hover:bg-gray-900"
      >
        {busy && !dateChanged ? (
          <span className="inline-flex items-center gap-2">
            <span
              aria-hidden="true"
              className="size-4 animate-spin rounded-full border-2 border-gray-300 border-t-blue-600 dark:border-gray-700 dark:border-t-blue-400"
            />
            Обновление…
          </span>
        ) : (
          dateChanged ? 'Показать' : 'Обновить'
        )}
      </button>
      {fetchedAt && (
        <span className="text-sm text-gray-500 dark:text-gray-400">
          данные на{' '}
          {new Date(fetchedAt).toLocaleTimeString('ru-RU', {
            hour: '2-digit',
            minute: '2-digit',
            hourCycle: 'h23',
          })}
          {cached && ' (из кэша)'}
        </span>
      )}
      <label className="inline-flex cursor-pointer items-center gap-2 text-sm text-gray-600 dark:text-gray-300">
        <input
          type="checkbox"
          checked={hideSuccessful}
          onChange={(event) => onHideSuccessfulChange(event.target.checked)}
          className="size-4 cursor-pointer accent-blue-600"
        />
        Скрыть успешные
      </label>
    </form>
  )
}
