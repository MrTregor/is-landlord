import { describe, expect, it } from 'vitest'
import { cronText, statsText } from './jobs'

describe('плановые задачи (07)', () => {
  it('расписание по-русски', () => {
    expect(cronText('5 0 * * *')).toBe('ежедневно в 00:05')
    expect(cronText('*/15 * * * *')).toBe('каждые 15 минут')
    expect(cronText('15 * * * *')).toBe('каждый час в :15')
  })
  it('итоги запуска', () => {
    expect(statsText({ contracts: 12, created: 8, errors: ['x'] })).toBe('договоров 12, создано 8, ошибок 1')
  })
})
