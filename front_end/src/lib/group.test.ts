import { describe, it, expect } from 'vitest'
import { groupByEnvironment } from './group'
import type { Job } from '../types'

function job(environment: string, name: string, ok: boolean): Job {
  return {
    environment, name, ok,
    label: name.toUpperCase(),
    start: null, end: null, duration_seconds: null,
    node: '', nodes: [],
  }
}

describe('groupByEnvironment', () => {
  it('на пустом списке даёт пустой список', () => {
    expect(groupByEnvironment([])).toEqual([])
  })

  it('собирает задачи одной среды, даже если они не подряд', () => {
    // Бэк кладёт все провалы в начало, поэтому задачи одной среды
    // приходят двумя кусками. Группировка обязана это пережить.
    const jobs = [
      job('KT', 'MinIO VK Cloud', false),
      job('PROD KPO', 'MongoDB', false),
      job('KT', 'MinIO', true),
      job('KT', 'PostgreSQL', true),
    ]

    const groups = groupByEnvironment(jobs)

    expect(groups.map((g) => g.environment)).toEqual(['KT', 'PROD KPO'])
    expect(groups[0].jobs).toHaveLength(3)
  })

  it('ставит среду с провалом первой, наследуя порядок бэка', () => {
    const jobs = [
      job('PROD KPO', 'MongoDB', false),
      job('KT', 'MinIO', true),
    ]

    expect(groupByEnvironment(jobs)[0].environment).toBe('PROD KPO')
  })

  it('считает успешные и все задачи среды', () => {
    const jobs = [
      job('KT', 'MinIO VK Cloud', false),
      job('KT', 'MinIO', true),
      job('KT', 'PostgreSQL', true),
    ]

    const [kt] = groupByEnvironment(jobs)
    expect(kt.ok).toBe(2)
    expect(kt.total).toBe(3)
  })

  it('сохраняет порядок задач внутри среды', () => {
    const jobs = [
      job('KT', 'MinIO VK Cloud', false),
      job('KT', 'MinIO', true),
    ]

    expect(groupByEnvironment(jobs)[0].jobs.map((j) => j.name))
      .toEqual(['MinIO VK Cloud', 'MinIO'])
  })
})
