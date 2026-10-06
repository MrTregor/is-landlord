// Подписи значений перечислений (04 Модель данных, «Справочник значений для интерфейса»)
// и цвета меток статусов (13 Фронтенд, раздел 6). Подписи в шаблонах берутся только отсюда.
import type { components } from '@/api/schema'

type S = components['schemas']
export type Severity = 'success' | 'warn' | 'danger' | 'info' | 'secondary'

export const propertyKindLabels: Record<S['PropertyKind'], string> = {
  residential: 'Жилой',
  non_residential: 'Нежилой',
}

export const propertyStatusLabels: Record<S['PropertyStatus'], string> = {
  free: 'Свободен',
  reserved: 'Забронирован',
  rented: 'Сдан',
  repair: 'На ремонте',
}

export const fiscalModeLabels: Record<S['FiscalMode'], string> = {
  none: 'Чек не требуется',
  cloud_kkt: 'Облачная касса',
  npd_manual: 'Чек НПД (вручную)',
}

export const contractStatusLabels: Record<S['ContractStatus'], string> = {
  draft: 'Черновик',
  active: 'Действует',
  extended: 'Продлён',
  terminated: 'Расторгнут',
  finished: 'Завершён',
}

export const legalBasisLabels: Record<S['LegalBasis'], string> = {
  ch35: 'Договор найма жилого помещения',
  ch34: 'Договор аренды',
}

export const utilitiesModeLabels: Record<S['UtilitiesMode'], string> = {
  included: 'Включены в плату',
  meters: 'По счётчикам',
  fixed: 'Фиксированная сумма',
}

const chargeKindBase: Record<S['ChargeKind'], string> = {
  rent: 'Плата за наём',
  utilities: 'Компенсация ЖКУ',
  penalty: 'Пени',
  damage: 'Возмещение ущерба',
  other: 'Прочее',
}

/** Вид начисления; для договоров аренды (гл. 34) — «Арендная плата». */
export function chargeKindLabel(kind: S['ChargeKind'], legalBasis?: S['LegalBasis']): string {
  if (kind === 'rent' && legalBasis === 'ch34') return 'Арендная плата'
  return chargeKindBase[kind]
}

export const chargeStatusLabels: Record<S['ChargeStatus'], string> = {
  open: 'Ожидает оплаты',
  partially_paid: 'Оплачено частично',
  paid: 'Оплачено',
  cancelled: 'Отменено',
}
export const overdueLabel = 'Просрочено'

export const penaltyStateLabels: Record<S['PenaltyState'], string> = {
  accruing: 'Начисляются',
  final: 'Итоговые',
  manual: 'Изменены вручную',
}

export const paymentMethodLabels: Record<S['PaymentMethod'], string> = {
  online: 'Онлайн (карта, СБП)',
  bank_transfer: 'Перевод на счёт',
  cash: 'Наличные',
  deposit_offset: 'Зачёт из залога',
}

export const paymentStatusLabels: Record<S['PaymentStatus'], string> = {
  pending: 'Ожидает оплаты',
  succeeded: 'Проведён',
  failed: 'Не прошёл',
  cancelled: 'Отменён',
}

export const depositOperationLabels: Record<S['DepositOperationKind'], string> = {
  receipt: 'Внесение',
  withholding: 'Удержание',
  offset: 'Зачёт в счёт долга',
  refund: 'Возврат',
}

export const meterTypeLabels: Record<S['MeterType'], string> = {
  cold_water: 'Холодная вода, м³',
  hot_water: 'Горячая вода, м³',
  electricity: 'Электроэнергия, кВт·ч',
  electricity_day: 'Электроэнергия (день), кВт·ч',
  electricity_night: 'Электроэнергия (ночь), кВт·ч',
  gas: 'Газ, м³',
  heating: 'Отопление, Гкал',
}

export const requestStatusLabels: Record<S['RequestStatus'], string> = {
  new: 'Новая',
  in_progress: 'В работе',
  done: 'Выполнена',
  closed: 'Закрыта',
}

export const requestCategoryLabels: Record<S['RequestCategory'], string> = {
  plumbing: 'Сантехника',
  electrical: 'Электрика',
  heating: 'Отопление',
  appliance: 'Бытовая техника',
  furniture: 'Мебель',
  doors_windows: 'Двери и окна',
  other: 'Другое',
}

export const expenseCategoryLabels: Record<S['ExpenseCategory'], string> = {
  repair: 'Ремонт',
  maintenance: 'Содержание',
  utilities_owner: 'ЖКУ за счёт собственника',
  tax: 'Налоги и сборы',
  insurance: 'Страхование',
  furniture_appliances: 'Мебель и техника',
  commission: 'Комиссии',
  other: 'Прочее',
}

export const discrepancyKindLabels: Record<S['DiscrepancyKind'], string> = {
  underpayment: 'Недоплата',
  overpayment: 'Переплата',
  unidentified: 'Неопознанный платёж',
}

export const fiscalStatusLabels: Record<S['FiscalStatus'], string> = {
  queued: 'В очереди',
  wait: 'Обрабатывается',
  done: 'Сформирован',
  fail: 'Ошибка',
  manual_required: 'Нужно сформировать вручную',
}

export const userRoleLabels: Record<S['UserRole'], string> = {
  owner: 'Собственник',
  tenant: 'Наниматель',
}

export const userStatusLabels: Record<S['UserStatus'], string> = {
  invited: 'Приглашён',
  active: 'Активен',
  blocked: 'Заблокирован',
}

// ---------------------------------------------------------------- Цвета меток статусов
export const contractStatusSeverity: Record<S['ContractStatus'], Severity> = {
  draft: 'warn',
  active: 'success',
  extended: 'success',
  terminated: 'danger',
  finished: 'secondary',
}

export const chargeStatusSeverity: Record<S['ChargeStatus'], Severity> = {
  open: 'info',
  partially_paid: 'warn',
  paid: 'success',
  cancelled: 'secondary',
}

export const propertyStatusSeverity: Record<S['PropertyStatus'], Severity> = {
  free: 'info',
  reserved: 'warn',
  rented: 'success',
  repair: 'secondary',
}

export const requestStatusSeverity: Record<S['RequestStatus'], Severity> = {
  new: 'info',
  in_progress: 'info',
  done: 'success',
  closed: 'secondary',
}

export const fiscalStatusSeverity: Record<S['FiscalStatus'], Severity> = {
  queued: 'info',
  wait: 'info',
  done: 'success',
  fail: 'danger',
  manual_required: 'warn',
}

export const paymentStatusSeverity: Record<S['PaymentStatus'], Severity> = {
  pending: 'info',
  succeeded: 'success',
  failed: 'danger',
  cancelled: 'secondary',
}
