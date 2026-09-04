// Зеркало DTO из back_end/internal/api/dto.go. Меняется только вместе с ним.

export type Job = {
  environment: string
  label: string
  name: string
  ok: boolean
  /** null у задач со статусом ERROR: команда не запускалась. */
  start: string | null
  end: string | null
  duration_seconds: number | null
  /** Нода, сделавшая бэкап. У проваленной задачи пуста. */
  node: string
  /** Все отчитавшиеся ноды. С бэка всегда массив, никогда null. */
  nodes: string[]
}

export type Stats = {
  messages: number
  backups: number
  skipped: number
}

export type ReportResponse = {
  date: string
  fetched_at: string
  cached: boolean
  stats: Stats
  jobs: Job[]
}
