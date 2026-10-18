import { describe, expect, it } from 'vitest'
import { cronText, formatSize, statsText } from './jobs'

describe('плановые задачи (07)', () => {
  it('расписание по-русски', () => {
    expect(cronText('5 0 * * *')).toBe('ежедневно в 00:05')
    expect(cronText('*/15 * * * *')).toBe('каждые 15 минут')
    expect(cronText('15 * * * *')).toBe('каждый час в :15')
  })
  it('итоги запуска', () => {
    expect(statsText({ contracts: 12, created: 8, errors: ['x'] })).toBe('договоров 12, создано 8, ошибок 1')
    expect(statsText({ sent: 1 })).toBe('отправлено 1')
    expect(statsText({ reminders: 2, overdue: 1, already_sent: 3 })).toBe(
      'напоминаний об оплате 2, писем о просрочке 1, уже отправлено ранее 3',
    )
  })
})

describe('размер резервной копии', () => {
  it('в байтах, КБ и МБ', () => {
    expect(formatSize(512)).toBe('512 Б')
    expect(formatSize(12_697)).toBe('12,4 КБ')
    expect(formatSize(1_572_864)).toBe('1,5 МБ')
  })
})
