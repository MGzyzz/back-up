// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import App from './App'
import { fetchReport } from './api/client'
import type { ReportResponse } from './types'

vi.mock('./api/client', async (original) => ({
  ...await original<typeof import('./api/client')>(),
  fetchReport: vi.fn(),
}))

let container: HTMLDivElement
let root: Root

function report(date: string, cached = false): ReportResponse {
  return {
    date, cached, fetched_at: `${date}T01:00:00+05:00`,
    stats: { messages: 0, backups: 0, skipped: 0 }, jobs: [],
  }
}

function deferred() {
  let resolve!: (value: ReportResponse) => void
  const promise = new Promise<ReportResponse>((done) => { resolve = done })
  return { promise, resolve }
}

function dateInput() { return container.querySelector<HTMLInputElement>('input[type=date]')! }
function refreshButton() { return container.querySelector<HTMLButtonElement>('button[type=submit]')! }

async function editDate(value: string) {
  await act(async () => {
    // Нативный setter моделирует ввод пользователя, обходя трекер React.
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(dateInput(), value)
    dateInput().dispatchEvent(new Event('input', { bubbles: true }))
  })
}

async function commitDate() {
  await act(async () => { dateInput().dispatchEvent(new FocusEvent('focusout', { bubbles: true })) })
}

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.mocked(fetchReport).mockReset()
  window.history.replaceState(null, '', '/?date=2026-09-06')
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})

afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.unstubAllGlobals()
})

it.each(['restore', 'clear'])('завершает загрузку после редактирования даты: %s', async (action) => {
  const pending = deferred()
  vi.mocked(fetchReport).mockReturnValueOnce(pending.promise)
  await act(async () => root.render(<App />))
  await editDate('2026-09-05')
  await editDate(action === 'restore' ? '2026-09-06' : '')
  await commitDate()
  await act(async () => pending.resolve(report('2026-09-06')))
  expect(dateInput().value).toBe('2026-09-06')
  expect(refreshButton().disabled).toBe(false)
  expect(container.textContent).toContain('За 2026-09-06')
  expect(fetchReport).toHaveBeenCalledTimes(1)
})

it('не заменяет выбранный день запоздавшим ответом', async () => {
  const first = deferred()
  const second = deferred()
  vi.mocked(fetchReport).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
  await act(async () => root.render(<App />))
  await editDate('2026-09-05')
  await commitDate()
  await act(async () => second.resolve(report('2026-09-05')))
  await act(async () => first.resolve(report('2026-09-06')))
  expect(container.textContent).toContain('За 2026-09-05')
  expect(container.textContent).not.toContain('За 2026-09-06')
  expect(refreshButton().disabled).toBe(false)
  expect(fetchReport).toHaveBeenCalledTimes(2)
})

it('показывает отметку кэша без дополнительного запроса', async () => {
  vi.mocked(fetchReport).mockResolvedValueOnce(report('2026-09-06', true))
  await act(async () => root.render(<App />))
  expect(container.textContent).toContain('(из кэша)')
  expect(fetchReport).toHaveBeenCalledTimes(1)
})
