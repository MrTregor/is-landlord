import { describe, expect, it } from 'vitest'
import { groupContracts, isSimilar, normalizeName } from './suggest'

describe('подсказки договоров по плательщику (FR-07.3)', () => {
  it('нормализует имя: регистр, Ё, ИП, кавычки', () => {
    expect(normalizeName('ИП «Семёнова» А. П.')).toEqual(['СЕМЕНОВА', 'А', 'П'])
  })

  it('похож по фамилии и по наименованию организации', () => {
    expect(isSimilar('Иванова Т. В.', 'Иванова Татьяна Викторовна')).toBe(true)
    expect(isSimilar('КУЗНЕЦОВ ПАВЕЛ', 'Кузнецов П. И.')).toBe(true)
    expect(isSimilar('ООО «Ромашка»', 'ООО «Ромашка»')).toBe(true)
    expect(isSimilar('Петров', 'Иванова Т. В.')).toBe(false)
    expect(isSimilar(null, 'Иванова Т. В.')).toBe(false)
  })

  it('сначала похожие, затем остальные; пустые группы скрыты', () => {
    const list = [
      { value: '1', label: '№ 1', tenant: 'Смирнова А. С.' },
      { value: '2', label: '№ 2', tenant: 'Иванов И. И.' },
    ]
    expect(groupContracts('Смирнова Анна', list).map((g) => g.items.map((i) => i.value))).toEqual([['1'], ['2']])
    expect(groupContracts('Петров', list)).toHaveLength(1)
  })
})
