import type { ReportResponse } from '../types'

/** Ошибка с текстом от сервера и, если он просил подождать, — со сколькими секундами. */
export class ApiError extends Error {
  // Параметрическое свойство конструктора здесь не годится: tsconfig
  // включает erasableSyntaxOnly, а такая сокращённая запись не стирается
  // при компиляции без типов — поэтому поле объявлено и присвоено явно.
  readonly retryAfter?: number
  constructor(message: string, retryAfter?: number) {
    super(message)
    this.name = 'ApiError'
    this.retryAfter = retryAfter
  }
}

export async function fetchReport(date: string): Promise<ReportResponse> {
  const res = await fetch(`/api/report?date=${encodeURIComponent(date)}`)

  if (!res.ok) {
    // Тело ошибки всегда {"error": "..."} — но сеть могла оборваться
    // на полуслове, поэтому разбор защищаем.
    let message = `сервер ответил ${res.status}`
    try {
      const body = await res.json()
      if (body?.error) message = body.error
    } catch {
      // оставляем сообщение по коду ответа
    }
    const retryAfter = Number(res.headers.get('Retry-After'))
    throw new ApiError(message, Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : undefined)
  }

  return res.json()
}
