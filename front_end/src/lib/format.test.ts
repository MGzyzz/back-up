import { describe, it, expect } from 'vitest'
import { formatTime, formatDuration } from './format'

describe('formatTime', () => {
  it('показывает время без даты', () => {
    expect(formatTime('2026-09-04T01:00:01+05:00')).toBe('01:00:01')
  })

  it('на отсутствующем времени даёт прочерк', () => {
    expect(formatTime(null)).toBe('—')
  })
})

describe('formatDuration', () => {
  it('переводит секунды в часы и минуты как в отчёте', () => {
    expect(formatDuration(4999)).toBe('1h 23m')
  })

  it('дополняет минуты нулём', () => {
    expect(formatDuration(300)).toBe('0h 05m')
  })

  it('на отсутствующей длительности даёт прочерк', () => {
    expect(formatDuration(null)).toBe('—')
  })
})
