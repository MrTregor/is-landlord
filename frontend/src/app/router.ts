// Маршруты (13 Фронтенд, раздел 4). Раскладка страницы задаётся meta.layout, доступ — meta.access.
import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { installGuards } from './guards'

export type Layout = 'auth' | 'owner' | 'cabinet' | 'blank'
export type Access = 'public' | 'owner' | 'tenant'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    layout?: Layout
    access?: Access
    /** Раздел меню рабочего места, к которому относится страница. */
    section?: string
  }
}

function page(path: string, title: string, section: string, component: RouteRecordRaw['component']): RouteRecordRaw {
  return { path, component, meta: { title, section, layout: 'owner', access: 'owner' } } as RouteRecordRaw
}

function cabinetPage(path: string, title: string, component: RouteRecordRaw['component']): RouteRecordRaw {
  return { path, component, meta: { title, layout: 'cabinet', access: 'tenant' } } as RouteRecordRaw
}

export const routes: RouteRecordRaw[] = [
  // Общие экраны
  {
    path: '/login',
    name: 'login',
    component: () => import('@/modules/auth/pages/LoginPage.vue'),
    meta: { title: 'Вход', layout: 'auth', access: 'public' },
  },
  {
    path: '/forgot-password',
    component: () => import('@/modules/auth/pages/ForgotPasswordPage.vue'),
    meta: { title: 'Восстановление пароля', layout: 'auth', access: 'public' },
  },
  {
    path: '/reset-password/:token',
    component: () => import('@/modules/auth/pages/ResetPasswordPage.vue'),
    meta: { title: 'Новый пароль', layout: 'auth', access: 'public' },
  },
  {
    path: '/invite/:token',
    component: () => import('@/modules/auth/pages/InvitePage.vue'),
    meta: { title: 'Приглашение', layout: 'auth', access: 'public' },
  },
  {
    path: '/privacy',
    component: () => import('@/modules/auth/pages/PrivacyPage.vue'),
    meta: { title: 'Политика обработки персональных данных', layout: 'blank', access: 'public' },
  },

  // Рабочее место собственника
  page('/', 'Сводка', 'dashboard', () => import('@/modules/dashboard/pages/DashboardPage.vue')),
  page('/properties', 'Объекты', 'properties', () => import('@/modules/properties/pages/PropertiesListPage.vue')),
  page(
    '/properties/new',
    'Новый объект',
    'properties',
    () => import('@/modules/properties/pages/PropertyFormPage.vue'),
  ),
  page('/properties/:id', 'Объект', 'properties', () => import('@/modules/properties/pages/PropertyCardPage.vue')),
  page(
    '/properties/:id/edit',
    'Изменение объекта',
    'properties',
    () => import('@/modules/properties/pages/PropertyFormPage.vue'),
  ),
  page('/tenants', 'Наниматели', 'tenants', () => import('@/modules/tenants/pages/TenantsListPage.vue')),
  page('/tenants/new', 'Новый наниматель', 'tenants', () => import('@/modules/tenants/pages/TenantFormPage.vue')),
  page('/tenants/:id', 'Наниматель', 'tenants', () => import('@/modules/tenants/pages/TenantCardPage.vue')),
  page(
    '/tenants/:id/edit',
    'Изменение нанимателя',
    'tenants',
    () => import('@/modules/tenants/pages/TenantFormPage.vue'),
  ),
  page('/contracts', 'Договоры', 'contracts', () => import('@/modules/contracts/pages/ContractsListPage.vue')),
  page(
    '/contracts/new',
    'Новый договор',
    'contracts',
    () => import('@/modules/contracts/pages/ContractWizardPage.vue'),
  ),
  page('/contracts/:id', 'Договор', 'contracts', () => import('@/modules/contracts/pages/ContractCardPage.vue')),
  page(
    '/contracts/:id/edit',
    'Изменение договора',
    'contracts',
    () => import('@/modules/contracts/pages/ContractWizardPage.vue'),
  ),
  page(
    '/contracts/:id/settlement',
    'Взаиморасчёт',
    'contracts',
    () => import('@/modules/contracts/pages/SettlementPage.vue'),
  ),
  page('/charges', 'Начисления', 'charges', () => import('@/modules/charges/pages/ChargesPage.vue')),
  page('/payments', 'Платежи', 'payments', () => import('@/modules/payments/pages/PaymentsPage.vue')),
  page('/payments/:id', 'Платёж', 'payments', () => import('@/modules/payments/pages/PaymentCardPage.vue')),
  page(
    '/reconciliation',
    'Сверка',
    'reconciliation',
    () => import('@/modules/reconciliation/pages/ReconciliationPage.vue'),
  ),
  page(
    '/reconciliation/statements/:id',
    'Выписка банка',
    'reconciliation',
    () => import('@/modules/reconciliation/pages/StatementPage.vue'),
  ),
  page('/readings', 'Показания счётчиков', 'readings', () => import('@/modules/readings/pages/ReadingsPage.vue')),
  page('/requests', 'Заявки', 'requests', () => import('@/modules/requests/pages/RequestsPage.vue')),
  page('/requests/:id', 'Заявка', 'requests', () => import('@/modules/requests/pages/RequestCardPage.vue')),
  page('/expenses', 'Затраты', 'expenses', () => import('@/modules/expenses/pages/ExpensesPage.vue')),
  page('/calendar', 'Календарь', 'calendar', () => import('@/modules/calendar/pages/CalendarPage.vue')),
  page('/reports', 'Отчёты', 'reports', () => import('@/modules/reports/pages/ReportsPage.vue')),
  page('/reports/:name', 'Отчёт', 'reports', () => import('@/modules/reports/pages/ReportPage.vue')),
  page('/fiscal-receipts', 'Чеки', 'fiscal', () => import('@/modules/fiscal/pages/FiscalReceiptsPage.vue')),
  {
    path: '/audit',
    component: () => import('@/modules/audit/pages/AuditPage.vue'),
    meta: { title: 'Журнал действий', section: 'audit', layout: 'owner', access: 'owner' },
  },
  page('/email-log', 'Письма', 'email', () => import('@/modules/email-log/pages/EmailLogPage.vue')),
  page(
    '/settings/document-templates/:kind',
    'Шаблон документа',
    'settings',
    () => import('@/modules/settings/pages/DocumentTemplatePage.vue'),
  ),
  page(
    '/settings/email-templates/:code',
    'Шаблон письма',
    'settings',
    () => import('@/modules/settings/pages/EmailTemplatePage.vue'),
  ),
  {
    path: '/settings/:tab?',
    component: () => import('@/modules/settings/pages/SettingsPage.vue'),
    meta: { title: 'Настройки', section: 'settings', layout: 'owner', access: 'owner' },
  },

  // Личный кабинет нанимателя
  cabinetPage('/cabinet', 'Главная', () => import('@/modules/cabinet/pages/CabinetHomePage.vue')),
  cabinetPage(
    '/cabinet/charges',
    'Начисления и платежи',
    () => import('@/modules/cabinet/pages/CabinetChargesPage.vue'),
  ),
  cabinetPage(
    '/cabinet/payments/:id/result',
    'Результат оплаты',
    () => import('@/modules/cabinet/pages/PaymentResultPage.vue'),
  ),
  cabinetPage('/cabinet/meters', 'Показания', () => import('@/modules/cabinet/pages/CabinetMetersPage.vue')),
  cabinetPage('/cabinet/requests', 'Заявки', () => import('@/modules/cabinet/pages/CabinetRequestsPage.vue')),
  cabinetPage(
    '/cabinet/requests/new',
    'Новая заявка',
    () => import('@/modules/cabinet/pages/CabinetRequestNewPage.vue'),
  ),
  cabinetPage('/cabinet/requests/:id', 'Заявка', () => import('@/modules/cabinet/pages/CabinetRequestPage.vue')),
  cabinetPage(
    '/cabinet/contract',
    'Договор и документы',
    () => import('@/modules/cabinet/pages/CabinetContractPage.vue'),
  ),
  cabinetPage('/cabinet/profile', 'Профиль', () => import('@/modules/cabinet/pages/CabinetProfilePage.vue')),

  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('@/components/NotFoundPage.vue'),
    meta: { title: 'Страница не найдена', layout: 'blank', access: 'public' },
  },
]

export function createAppRouter() {
  const router = createRouter({ history: createWebHistory(), routes })
  installGuards(router)
  return router
}
