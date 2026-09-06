import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchReport, ApiError } from './api/client'
import { groupByEnvironment } from './lib/group'
import { todayISO } from './lib/date'
import type { ReportResponse } from './types'
import { DateBar } from './components/DateBar'
import { EnvSection } from './components/EnvSection'
import { ThemeToggle } from './components/ThemeToggle'

/**
 * refreshing отделён от loading намеренно: при смене даты экран пустеет
 * и показывает скелет, при нажатии «обновить» данные остаются на месте.
 * Иначе каждое обновление мигает пустотой.
 */
type State =
  | { kind: 'loading' }
  | { kind: 'refreshing'; data: ReportResponse }
  | { kind: 'ok'; data: ReportResponse }
  | { kind: 'error'; message: string; retryAfter?: number }

/** Дата берётся из адреса, чтобы ссылку можно было переслать. */
function dateFromLocation(): string {
  const fromUrl = new URLSearchParams(window.location.search).get('date')
  return fromUrl && /^\d{4}-\d{2}-\d{2}$/.test(fromUrl) ? fromUrl : todayISO()
}

export default function App() {
  const [date, setDate] = useState(dateFromLocation)
  // Поле календаря меняется отдельно от даты отчёта. Иначе некоторые
  // браузеры отправляют запросы за промежуточные даты при листании месяцев.
  const [draftDate, setDraftDate] = useState(dateFromLocation)
  const [state, setState] = useState<State>({ kind: 'loading' })
  const [hideSuccessful, setHideSuccessful] = useState(false)
  const requestVersion = useRef(0)

  const load = useCallback(
    async (day: string, keepData: boolean) => {
      const version = ++requestVersion.current
      setState((prev) =>
        keepData && (prev.kind === 'ok' || prev.kind === 'refreshing')
          ? { kind: 'refreshing', data: prev.data }
          : { kind: 'loading' },
      )
      try {
        const data = await fetchReport(day)
        if (version === requestVersion.current) {
          setState({ kind: 'ok', data })
        }
      } catch (err) {
        if (version !== requestVersion.current) return
        const message = err instanceof Error ? err.message : 'неизвестная ошибка'
        const retryAfter = err instanceof ApiError ? err.retryAfter : undefined
        setState({ kind: 'error', message, retryAfter })
      }
    },
    [],
  )

  useEffect(() => {
    const url = new URL(window.location.href)
    url.searchParams.set('date', date)
    window.history.replaceState(null, '', url)
    load(date, false)
  }, [date, load])

  const data = state.kind === 'ok' || state.kind === 'refreshing' ? state.data : null
  const groups = data ? groupByEnvironment(data.jobs) : []
  const visibleGroups = hideSuccessful
    ? groups
        .map((group) => ({ ...group, jobs: group.jobs.filter((job) => !job.ok) }))
        .filter((group) => group.jobs.length > 0)
    : groups
  const isFutureDateError =
    state.kind === 'error' && state.message.includes('день ещё не наступил')

  return (
    <main className="mx-auto max-w-4xl p-6">
      <header className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold">Бэкапы</h1>
        <ThemeToggle />
      </header>

      <DateBar
        date={draftDate}
        // Редактирование черновика не отменяет загрузку выбранного отчёта.
        // Устаревший ответ отсекается в load, когда начинается новый запрос.
        onDateChange={setDraftDate}
        onDateCommit={() => {
          // Поле очистили — вернуть последнюю рабочую дату, а не запрашивать пустую.
          if (!draftDate) {
            setDraftDate(date)
            return
          }
          // Тот же день не перезапрашиваем: для этого есть «Обновить».
          if (draftDate !== date) setDate(draftDate)
        }}
        onRefresh={() => {
          if (draftDate === date) {
            load(date, true)
          } else {
            setDate(draftDate)
          }
        }}
        busy={state.kind === 'loading' || state.kind === 'refreshing'}
        fetchedAt={data?.fetched_at ?? null}
        cached={data?.cached ?? false}
        hideSuccessful={hideSuccessful}
        onHideSuccessfulChange={setHideSuccessful}
      />

      {state.kind === 'loading' && (
        <div className="space-y-4">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-24 animate-pulse rounded bg-gray-100 dark:bg-gray-800" />
          ))}
        </div>
      )}

      {isFutureDateError && (
        <div className="py-16 text-center text-gray-500 dark:text-gray-400">
          <p className="text-lg">Выбранный день ещё не наступил.</p>
          <p className="mt-1 text-sm">Выберите сегодняшнюю или более раннюю дату.</p>
        </div>
      )}

      {state.kind === 'error' && !isFutureDateError && (
        <div className="rounded border border-red-200 bg-red-50 p-4 dark:border-red-900 dark:bg-red-950/40">
          <p className="mb-3 text-red-800 dark:text-red-300">{state.message}</p>
          {state.retryAfter && (
            <p className="mb-3 text-sm text-red-700 dark:text-red-400">
              Повторить можно через {state.retryAfter} с.
            </p>
          )}
          <button
            onClick={() => load(date, false)}
            className="rounded border border-red-300 px-3 py-1.5 text-sm hover:bg-red-100 dark:border-red-800 dark:hover:bg-red-950"
          >
            Повторить
          </button>
        </div>
      )}

      {data && groups.length === 0 && (
        // Пустой день — не ошибка: канал мог молчать или день ещё не начался.
        // Отдельно от сломанной сессии, иначе её молча спрячет «нет данных».
        <p className="text-gray-500 dark:text-gray-400">
          За {data.date} в канале нет сообщений о бэкапах.
        </p>
      )}

      {data && hideSuccessful && groups.length > 0 && visibleGroups.length === 0 && (
        <p className="py-16 text-center text-gray-500 dark:text-gray-400">
          Все бэкапы выполнены успешно.
        </p>
      )}

      {visibleGroups.map((group) => (
        <EnvSection key={group.environment} group={group} />
      ))}
    </main>
  )
}
