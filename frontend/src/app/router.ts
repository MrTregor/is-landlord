// Маршруты (13 Фронтенд, раздел 4). Раскладка страницы задаётся meta.layout, доступ — meta.access.
// Экраны, которые делаются на следующих этапах, пока открывают заглушку PlannedPage с номером этапа.
import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import PlannedPage from '@/components/PlannedPage.vue'
import { installGuards } from './guards'

export type Layout = 'auth' | 'owner' | 'cabinet' | 'blank'
export type Access = 'public' | 'owner' | 'tenant'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    layout?: Layout
    access?: Access
    /** Этап плана (19 План реализации), на котором появляется экран. */
    stage?: string
    /** Раздел меню рабочего места, к которому относится страница. */
    section?: string
  }
}

function owner(path: string, title: string, stage: string, section: string): RouteRecordRaw {
  return { path, component: PlannedPage, meta: { title, stage, section, layout: 'owner', access: 'owner' } }
}

function page(path: string, title: string, section: string, component: RouteRecordRaw['component']): RouteRecordRaw {
  return { path, component, meta: { title, section, layout: 'owner', access: 'owner' } } as RouteRecordRaw
}

function cabinetPage(path: string, title: string, component: RouteRecordRaw['component']): RouteRecordRaw {
  return { path, component, meta: { title, layout: 'cabinet', access: 'tenant' } } as RouteRecordRaw
}

function cabinet(path: string, title: string, stage: string): RouteRecordRaw {
  return { path, component: PlannedPage, meta: { title, stage, layout: 'cabinet', access: 'tenant' } }
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
  owner('/', 'Сводка', 'M12', 'dashboard'),
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
  owner('/reconciliation/statements/:id', 'Выписка банка', 'M9', 'reconciliation'),
  page('/readings', 'Показания счётчиков', 'readings', () => import('@/modules/readings/pages/ReadingsPage.vue')),
  owner('/requests', 'Заявки', 'M11', 'requests'),
  owner('/requests/:id', 'Заявка', 'M11', 'requests'),
  owner('/expenses', 'Затраты', 'M11', 'expenses'),
  page('/calendar', 'Календарь', 'calendar', () => import('@/modules/calendar/pages/CalendarPage.vue')),
  owner('/reports', 'Отчёты', 'M12', 'reports'),
  owner('/reports/:name', 'Отчёт', 'M12', 'reports'),
  page('/fiscal-receipts', 'Чеки', 'fiscal', () => import('@/modules/fiscal/pages/FiscalReceiptsPage.vue')),
  {
    path: '/audit',
    component: () => import('@/modules/audit/pages/AuditPage.vue'),
    meta: { title: 'Журнал действий', section: 'audit', layout: 'owner', access: 'owner' },
  },
  owner('/email-log', 'Письма', 'M10', 'email'),
  page(
    '/settings/document-templates/:kind',
    'Шаблон документа',
    'settings',
    () => import('@/modules/settings/pages/DocumentTemplatePage.vue'),
  ),
  owner('/settings/email-templates/:code', 'Шаблон письма', 'M10', 'settings'),
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
  cabinet('/cabinet/meters', 'Показания', 'M13'),
  cabinet('/cabinet/requests', 'Заявки', 'M13'),
  cabinet('/cabinet/requests/new', 'Новая заявка', 'M13'),
  cabinet('/cabinet/requests/:id', 'Заявка', 'M13'),
  cabinet('/cabinet/contract', 'Договор и документы', 'M13'),
  cabinet('/cabinet/profile', 'Профиль', 'M13'),

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
