// Отчёты (11 Отчёты): названия, описания в одну строку и вид параметров.

export type ReportName = 'income-expense' | 'occupancy' | 'debts' | 'tax-base' | 'income-book'
export type ParamKind = 'period' | 'asOf' | 'year'

export const reports: { name: ReportName; title: string; description: string; params: ParamKind }[] = [
  {
    name: 'income-expense',
    title: 'Доходы и расходы по объектам',
    description: 'Поступления по видам, расходы, финансовый результат, рентабельность и заполняемость',
    params: 'period',
  },
  {
    name: 'occupancy',
    title: 'Заполняемость и простои',
    description: 'Дни сдачи и простоя по объектам, периоды простоя и недополученный доход',
    params: 'period',
  },
  {
    name: 'debts',
    title: 'Задолженность по нанимателям',
    description: 'Долг по возрасту на дату, пени, остаток залога, аванс и случаи просрочки',
    params: 'asOf',
  },
  {
    name: 'tax-base',
    title: 'Налоговая база',
    description: 'Доходы по кварталам и нарастающим итогом, налог по режиму налогообложения',
    params: 'year',
  },
  {
    name: 'income-book',
    title: 'Доходы для книги учёта',
    description: 'Операции за год для переноса в раздел I книги учёта доходов и расходов',
    params: 'year',
  },
]
