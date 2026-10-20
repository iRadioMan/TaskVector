import { describe, expect, it } from 'vitest'
import { formatDateTime } from './dates'

describe('formatDateTime', () => {
  it('formats an absolute deadline in the selected profile timezone', () => {
    const value = formatDateTime('2026-03-29T01:30:00.000Z', 'Europe/Paris', 'en-GB')
    expect(value).toContain('03:30')
    expect(value).toContain('29 Mar 2026')
  })
})
