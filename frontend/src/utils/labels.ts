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

export const paymentPurposeLabels: Record<S['PaymentPurpose'], string> = {
  charges: 'Начисления',
  deposit: 'Залог',
}

/** Назначение в диалоге регистрации платежа (14 разд. 6.2). */
export const paymentPurposeFormLabels: Record<S['PaymentPurpose'], string> = {
  charges: 'Оплата начислений',
  deposit: 'Обеспечительный платёж',
}

export const paymentSourceLabels: Record<S['PaymentSource'], string> = {
  online: 'Онлайн',
  manual: 'Вручную',
  bank_import: 'Выписка',
  deposit_offset: 'Зачёт залога',
}

export const manualPaymentMethodLabels: Record<S['ManualPaymentMethod'], string> = {
  bank_transfer: 'Перевод на счёт',
  cash: 'Наличные',
}

export const refundMethodLabels: Record<S['RefundMethod'], string> = {
  bank_transfer: 'Перевод на счёт',
  cash: 'Наличные',
}

export const discrepancyStatusLabels: Record<S['DiscrepancyStatus'], string> = {
  open: 'Открытые',
  resolved: 'Решённые',
}

export const discrepancyResolutionLabels: Record<S['DiscrepancyResolution'], string> = {
  linked: 'Привязан к договору',
  refunded: 'Возвращено',
  kept_as_advance: 'Оставлено авансом',
  moved_to_deposit: 'Перенесено в залог',
  acknowledged: 'Принято к сведению',
  ignored: 'Не относится к аренде',
  auto_closed: 'Закрыто автоматически',
}

/** Статусы строк выписки (14 Экраны собственника, раздел 7.1). */
export const bankLineStatusLabels: Record<S['BankLineStatus'], string> = {
  matched: 'Сопоставлен',
  possible_duplicate: 'Возможный дубль',
  unmatched: 'Не опознан',
  duplicate: 'Дубль',
  linked_manual: 'Привязан вручную',
  refunded: 'Возвращён плательщику',
  ignored: 'Не относится к аренде',
}

export const matchMethodLabels: Record<S['MatchMethod'], string> = {
  contract_number: 'По номеру договора',
  payer_inn: 'По ИНН',
  payer_name: 'По имени плательщика',
  manual: 'Вручную',
}

export const statementFormatLabels: Record<S['StatementFormat'], string> = {
  '1c': '1С',
  csv: 'CSV',
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

export const requestPriorityLabels: Record<S['RequestPriority'], string> = {
  low: 'Низкий',
  normal: 'Обычный',
  high: 'Высокий',
}

/** Возмещение затраты за счёт нанимателя (14 Экраны собственника, раздел 10). */
export const expenseRecoveryLabels: Record<S['ExpenseRecovery'], string> = {
  none: 'Нет',
  deposit: 'Из залога',
  charge: 'Начисление',
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

export const emailStatusLabels: Record<S['EmailStatus'], string> = {
  queued: 'В очереди',
  sent: 'Отправлено',
  failed: 'Ошибка',
  skipped: 'Пропущено',
}

export const emailRecipientLabels: Record<S['EmailRecipient'], string> = {
  tenant: 'Нанимателю',
  owner: 'Собственнику',
  user: 'Пользователю',
}

export const fiscalStatusLabels: Record<S['FiscalStatus'], string> = {
  queued: 'В очереди',
  wait: 'Обрабатывается',
  done: 'Сформирован',
  fail: 'Ошибка',
  manual_required: 'Нужно сформировать вручную',
}

export const fiscalOperationLabels: Record<S['FiscalOperation'], string> = {
  sell: 'Приход',
  sell_refund: 'Возврат прихода',
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

export const emailStatusSeverity: Record<S['EmailStatus'], Severity> = {
  queued: 'info',
  sent: 'success',
  failed: 'danger',
  skipped: 'secondary',
}

export const bankLineStatusSeverity: Record<S['BankLineStatus'], Severity> = {
  matched: 'success',
  possible_duplicate: 'warn',
  unmatched: 'danger',
  duplicate: 'secondary',
  linked_manual: 'success',
  refunded: 'secondary',
  ignored: 'secondary',
}

/** Виды доходных операций (05 разд. 12.1). */
export const incomeKindLabels: Record<S['IncomeKind'], string> = {
  rent: 'Плата за наём',
  utilities: 'Компенсация ЖКУ',
  penalty: 'Пени',
  damage: 'Возмещение ущерба',
  other: 'Прочее',
  advance: 'Аванс',
  deposit_withholding: 'Удержание из залога',
  refund: 'Возврат переплаты',
  advance_to_deposit: 'Перенос переплаты в залог',
}

export const taxRegimeLabels: Record<S['TaxRegime'], string> = {
  usn_income: 'УСН «доходы»',
  usn_income_expense: 'УСН «доходы минус расходы»',
  npd: 'НПД',
  psn: 'Патент',
  osn: 'ОСН',
}

export const auditActorRoleLabels: Record<S['AuditActorRole'], string> = {
  owner: 'Собственник',
  tenant: 'Наниматель',
  system: 'Система',
}

export const docTypeLabels: Record<S['DocType'], string> = {
  passport_rf: 'Паспорт РФ',
  foreign_passport: 'Иностранный паспорт',
  other: 'Другой',
}

export const tenantKindLabels: Record<S['TenantKind'], string> = {
  individual: 'Физлицо',
  legal: 'Юрлицо',
}

export const cabinetStatusLabels: Record<S['CabinetStatus'], string> = {
  none: 'нет',
  invited: 'приглашён',
  active: 'активен',
  blocked: 'заблокирован',
}

export const consentMethodLabels: Record<S['ConsentMethod'], string> = {
  paper: 'Бумажное',
  electronic: 'В личном кабинете',
}

/** Вид ресурса без единиц: «Холодная вода». */
export const meterTypeShort: Record<S['MeterType'], string> = {
  cold_water: 'Холодная вода',
  hot_water: 'Горячая вода',
  electricity: 'Электроэнергия',
  electricity_day: 'Электроэнергия (день)',
  electricity_night: 'Электроэнергия (ночь)',
  gas: 'Газ',
  heating: 'Отопление',
}

/** Вид договора кратко (список договоров). */
export const legalBasisShort: Record<S['LegalBasis'], string> = {
  ch35: 'Наём',
  ch34: 'Аренда',
}

export const terminationInitiatorLabels: Record<S['TerminationInitiator'], string> = {
  landlord: 'Наймодатель',
  tenant: 'Наниматель',
  mutual: 'По соглашению сторон',
}

export const amendmentKindLabels: Record<S['AmendmentKind'], string> = {
  indexation: 'Индексация платы',
  rent_change: 'Изменение платы',
  extension: 'Продление',
  terms_change: 'День оплаты, фиксированная часть ЖКУ',
  other: 'Иные изменения',
}

export const amendmentStatusLabels: Record<S['AmendmentStatus'], string> = {
  draft: 'Черновик',
  signed: 'Подписано',
  cancelled: 'Отменено',
}

export const amendmentStatusSeverity: Record<S['AmendmentStatus'], Severity> = {
  draft: 'warn',
  signed: 'success',
  cancelled: 'secondary',
}

export const actKindLabels: Record<S['ActKind'], string> = {
  handover: 'Акт приёма-передачи',
  return: 'Акт возврата',
}

export const documentKindLabels: Record<S['DocumentKind'], string> = {
  contract_ch35: 'Договор найма жилого помещения',
  contract_ch34: 'Договор аренды',
  act_handover: 'Акт приёма-передачи',
  act_return: 'Акт возврата',
  amendment: 'Дополнительное соглашение',
  payment_receipt: 'Квитанция об оплате',
  reconciliation_act: 'Акт сверки',
  pdn_consent: 'Согласие на обработку ПДн',
}

export const calendarKindLabels: Record<S['CalendarEventKind'], string> = {
  payment_due: 'Сроки оплаты',
  overdue: 'Просрочки',
  contract_end: 'Окончание договоров',
  contract_end_notice: 'Уведомления',
  registration: 'Регистрация',
  indexation: 'Индексация',
  meter_verification: 'Поверка',
  showing: 'Показы',
  repair: 'Ремонты',
  custom: 'Прочее',
}

export const manualEventKindLabels: Record<S['ManualCalendarEventKind'], string> = {
  showing: 'Показ',
  repair: 'Плановый ремонт',
  custom: 'Прочее',
}

export const adjustmentKindLabels: Record<S['AdjustmentKind'], string> = {
  manual: 'Корректировка',
  termination: 'Расторжение договора',
  terms_change: 'Изменение условий',
  penalty_recalc: 'Пересчёт пеней',
  penalty_manual_set: 'Изменение суммы пеней',
  penalty_writeoff: 'Списание пеней',
}

export const chargeOriginLabels: Record<S['ChargeOrigin'], string> = {
  auto: 'авто',
  manual: 'вручную',
}

export const manualChargeKindLabels: Record<S['ManualChargeKind'], string> = {
  damage: 'Возмещение ущерба',
  other: 'Прочее',
}

export const readingStatusLabels: Record<S['ReadingStatus'], string> = {
  submitted: 'На проверке',
  accepted: 'Принято',
  rejected: 'Отклонено',
}

export const readingStatusSeverity: Record<S['ReadingStatus'], Severity> = {
  submitted: 'warn',
  accepted: 'success',
  rejected: 'danger',
}

export const readingSourceLabels: Record<S['ReadingSource'], string> = {
  owner: 'Собственник',
  tenant: 'Наниматель',
  act_handover: 'Акт приёма-передачи',
  act_return: 'Акт возврата',
}

export const jobRunStatusLabels: Record<S['JobRunStatus'], string> = {
  running: 'Выполняется',
  succeeded: 'Успешно',
  failed: 'С ошибками',
}

export const jobRunStatusSeverity: Record<S['JobRunStatus'], Severity> = {
  running: 'info',
  succeeded: 'success',
  failed: 'danger',
}

export const jobTriggerLabels: Record<S['JobTrigger'], string> = {
  schedule: 'по расписанию',
  manual: 'вручную',
}

/** Варианты для выпадающего списка из словаря подписей. */
export function options<K extends string>(labels: Record<K, string>): { value: K; label: string }[] {
  return (Object.entries(labels) as [K, string][]).map(([value, label]) => ({ value, label }))
}
