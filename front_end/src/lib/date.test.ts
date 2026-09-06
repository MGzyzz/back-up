import { describe, expect, it } from 'vitest'
import { todayISO } from './date'

describe('todayISO', () => {
  it('сохраняет локальный день сразу после полуночи', () => {
    expect(todayISO(new Date(2026, 8, 7, 1, 0))).toBe('2026-09-07')
  })

  it('сохраняет локальный день перед полуночью и дополняет нули', () => {
    expect(todayISO(new Date(2026, 0, 2, 23, 59))).toBe('2026-01-02')
  })
})
