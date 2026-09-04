type Props = {
  date: string
  onDateChange: (date: string) => void
  onRefresh: () => void
  busy: boolean
  /** Когда сервер получил эти данные; null, пока данных нет. */
  fetchedAt: string | null
  cached: boolean
}

export function DateBar({ date, onDateChange, onRefresh, busy, fetchedAt, cached }: Props) {
  const today = new Date().toISOString().slice(0, 10)

  return (
    <div className="mb-6 flex flex-wrap items-center gap-3">
      <input
        type="date"
        value={date}
        max={today}
        onChange={(e) => onDateChange(e.target.value)}
        className="rounded border border-gray-300 px-3 py-1.5"
      />
      <button
        onClick={onRefresh}
        disabled={busy}
        className="rounded border border-gray-300 px-3 py-1.5 hover:bg-gray-50 disabled:opacity-50"
      >
        Обновить
      </button>
      {fetchedAt && (
        // Без этой подписи «обновить» выглядит сломанной, когда ответ
        // пришёл из кэша и ничего на экране не изменилось.
        <span className="text-sm text-gray-500">
          данные на {new Date(fetchedAt).toLocaleTimeString()}
          {cached && ' (из кэша)'}
        </span>
      )}
    </div>
  )
}
