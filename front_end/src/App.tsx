import { useCallback, useEffect, useState } from 'react'
import { fetchReport, ApiError } from './api/client'
import { groupByEnvironment } from './lib/group'
import type { ReportResponse } from './types'
import { DateBar } from './components/DateBar'
import { EnvSection } from './components/EnvSection'

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

function todayISO(): string {
  return new Date().toISOString().slice(0, 10)
}

/** Дата берётся из адреса, чтобы ссылку можно было переслать. */
function dateFromLocation(): string {
  const fromUrl = new URLSearchParams(window.location.search).get('date')
  return fromUrl && /^\d{4}-\d{2}-\d{2}$/.test(fromUrl) ? fromUrl : todayISO()
}

export default function App() {
  const [date, setDate] = useState(dateFromLocation)
  const [state, setState] = useState<State>({ kind: 'loading' })

  const load = useCallback(
    async (day: string, keepData: boolean) => {
      setState((prev) =>
        keepData && (prev.kind === 'ok' || prev.kind === 'refreshing')
          ? { kind: 'refreshing', data: prev.data }
          : { kind: 'loading' },
      )
      try {
        setState({ kind: 'ok', data: await fetchReport(day) })
      } catch (err) {
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

  return (
    <main className="mx-auto max-w-4xl p-6">
      <h1 className="mb-6 text-2xl font-bold">Бэкапы</h1>

      <DateBar
        date={date}
        onDateChange={setDate}
        onRefresh={() => load(date, true)}
        busy={state.kind === 'loading' || state.kind === 'refreshing'}
        fetchedAt={data?.fetched_at ?? null}
        cached={data?.cached ?? false}
      />

      {state.kind === 'refreshing' && (
        <div className="mb-4 h-0.5 animate-pulse rounded bg-blue-400" />
      )}

      {state.kind === 'loading' && (
        <div className="space-y-4">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-24 animate-pulse rounded bg-gray-100" />
          ))}
        </div>
      )}

      {state.kind === 'error' && (
        <div className="rounded border border-red-200 bg-red-50 p-4">
          <p className="mb-3 text-red-800">{state.message}</p>
          {state.retryAfter && (
            <p className="mb-3 text-sm text-red-700">
              Повторить можно через {state.retryAfter} с.
            </p>
          )}
          <button
            onClick={() => load(date, false)}
            className="rounded border border-red-300 px-3 py-1.5 text-sm hover:bg-red-100"
          >
            Повторить
          </button>
        </div>
      )}

      {data && groups.length === 0 && (
        // Пустой день — не ошибка: канал мог молчать или день ещё не начался.
        // Отдельно от сломанной сессии, иначе её молча спрячет «нет данных».
        <p className="text-gray-500">
          За {data.date} в канале нет сообщений о бэкапах.
        </p>
      )}

      {groups.map((group) => (
        <EnvSection key={group.environment} group={group} />
      ))}
    </main>
  )
}
