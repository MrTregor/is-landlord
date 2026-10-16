// Подсказки «договоры, где наниматель похож на плательщика» для привязки неопознанного платежа
// (14 Экраны собственника, раздел 7). Нормализация — как при сопоставлении строк выписки (05 разд. 15.1, шаг 4).

/** Верхний регистр, Ё → Е, без «ИП», «ООО», кавычек и знаков препинания. */
export function normalizeName(s: string): string[] {
  return s
    .toUpperCase()
    .replace(/Ё/g, 'Е')
    .replace(/["'«»„“”‘’.,]/g, ' ')
    .split(/\s+/)
    .filter((w) => w && w !== 'ИП' && w !== 'ООО' && w !== 'АО' && w !== 'ПАО')
}

/** Похож ли наниматель на плательщика: совпадает фамилия (первое слово) или наименование организации. */
export function isSimilar(payer: string | null | undefined, tenant: string): boolean {
  const p = normalizeName(payer ?? '')
  const t = normalizeName(tenant)
  if (!p.length || !t.length) return false
  if (p[0] === t[0]) return true
  return p.join(' ') === t.join(' ')
}

export interface ContractOption {
  value: string
  label: string
  tenant: string
}

/** Варианты выбора договора: сначала похожие на плательщика, затем остальные. */
export function groupContracts(payer: string | null | undefined, list: ContractOption[]) {
  const similar = list.filter((c) => isSimilar(payer, c.tenant))
  const other = list.filter((c) => !isSimilar(payer, c.tenant))
  return [
    { label: 'Похожие на плательщика', items: similar },
    { label: 'Все договоры', items: other },
  ].filter((g) => g.items.length)
}
