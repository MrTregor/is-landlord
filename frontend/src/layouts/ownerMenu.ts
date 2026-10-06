// Пункты меню рабочего места собственника (13 Фронтенд, раздел 5).
import type { components } from '@/api/schema'

type Dashboard = components['schemas']['Dashboard']

export interface MenuItem {
  section: string
  label: string
  icon: string
  to: string
  /** Число для значка из сводки. */
  badge?: (d: Dashboard) => number
}

export const ownerMenu: MenuItem[] = [
  { section: 'dashboard', label: 'Сводка', icon: 'pi pi-home', to: '/' },
  { section: 'properties', label: 'Объекты', icon: 'pi pi-building', to: '/properties' },
  { section: 'tenants', label: 'Наниматели', icon: 'pi pi-users', to: '/tenants' },
  { section: 'contracts', label: 'Договоры', icon: 'pi pi-file', to: '/contracts' },
  { section: 'charges', label: 'Начисления', icon: 'pi pi-calculator', to: '/charges' },
  { section: 'payments', label: 'Платежи', icon: 'pi pi-wallet', to: '/payments' },
  {
    section: 'reconciliation',
    label: 'Сверка',
    icon: 'pi pi-sync',
    to: '/reconciliation',
    badge: (d) => d.openDiscrepancies,
  },
  { section: 'readings', label: 'Показания', icon: 'pi pi-gauge', to: '/readings', badge: (d) => d.pendingReadings },
  { section: 'requests', label: 'Заявки', icon: 'pi pi-wrench', to: '/requests', badge: (d) => d.openRequests },
  { section: 'expenses', label: 'Затраты', icon: 'pi pi-shopping-cart', to: '/expenses' },
  { section: 'calendar', label: 'Календарь', icon: 'pi pi-calendar', to: '/calendar' },
  { section: 'reports', label: 'Отчёты', icon: 'pi pi-chart-bar', to: '/reports' },
  { section: 'fiscal', label: 'Чеки', icon: 'pi pi-receipt', to: '/fiscal-receipts', badge: (d) => d.fiscalProblems },
  { section: 'audit', label: 'Журнал действий', icon: 'pi pi-history', to: '/audit' },
  { section: 'email', label: 'Письма', icon: 'pi pi-envelope', to: '/email-log' },
  { section: 'settings', label: 'Настройки', icon: 'pi pi-cog', to: '/settings' },
]

export const cabinetMenu = [
  { label: 'Главная', icon: 'pi pi-home', to: '/cabinet' },
  { label: 'Начисления', icon: 'pi pi-list', to: '/cabinet/charges' },
  { label: 'Показания', icon: 'pi pi-gauge', to: '/cabinet/meters' },
  { label: 'Заявки', icon: 'pi pi-wrench', to: '/cabinet/requests' },
  { label: 'Профиль', icon: 'pi pi-user', to: '/cabinet/profile' },
]
