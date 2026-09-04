import type { Job } from '../types'

export type EnvGroup = {
  environment: string
  jobs: Job[]
  /** Сколько задач среды прошло. */
  ok: number
  total: number
}

/**
 * Группирует задачи по средам в порядке первого появления.
 *
 * Своей сортировки здесь нет и быть не должно. Бэк уже отдаёт задачи
 * отсортированными — сначала все провалы, потом всё успешное, — поэтому
 * порядок первого появления сам поднимает среду с провалом наверх,
 * а внутри среды провал оказывается первой строкой.
 *
 * Задачи одной среды приходят несколькими кусками именно из-за этой
 * сортировки, поэтому собираем по всему списку, а не подряд идущими
 * участками.
 */
export function groupByEnvironment(jobs: Job[]): EnvGroup[] {
  const byEnv = new Map<string, EnvGroup>()

  for (const job of jobs) {
    let group = byEnv.get(job.environment)
    if (!group) {
      group = { environment: job.environment, jobs: [], ok: 0, total: 0 }
      byEnv.set(job.environment, group)
    }
    group.jobs.push(job)
    group.total++
    if (job.ok) group.ok++
  }

  return [...byEnv.values()] // Map сохраняет порядок вставки
}
