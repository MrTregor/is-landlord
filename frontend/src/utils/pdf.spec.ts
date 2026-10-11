import { afterEach, describe, expect, it, vi } from 'vitest'
import { openInNewTab } from './pdf'

// FR-04.1: PDF открывается во вкладке, открытой сразу по нажатию; при ошибке вкладка закрывается.
describe('openInNewTab', () => {
  afterEach(() => vi.restoreAllMocks())

  it('подставляет адрес в заранее открытую вкладку', async () => {
    const tab = { location: { href: '' }, close: vi.fn() }
    vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
    await openInNewTab(async () => '/api/v1/documents/1/pdf')
    expect(tab.location.href).toBe('/api/v1/documents/1/pdf')
    expect(tab.close).not.toHaveBeenCalled()
  })

  it('закрывает вкладку при ошибке и при отмене', async () => {
    const tab = { location: { href: '' }, close: vi.fn() }
    vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
    await expect(openInNewTab(async () => Promise.reject(new Error('502')))).rejects.toThrow('502')
    await openInNewTab(async () => null)
    expect(tab.close).toHaveBeenCalledTimes(2)
  })
})
