// Открытие PDF в новой вкладке. Вкладка открывается сразу по нажатию (иначе браузер её заблокирует),
// адрес подставляется, когда документ сформирован; при ошибке или отмене вкладка закрывается.

/** Открыть адрес, который вернёт load (null — не открывать). Ошибка load пробрасывается дальше. */
export async function openInNewTab(load: () => Promise<string | null>): Promise<void> {
  const tab = window.open('', '_blank')
  try {
    const url = await load()
    if (!url) {
      tab?.close()
      return
    }
    if (tab) tab.location.href = url
    else window.open(url, '_blank')
  } catch (err) {
    tab?.close()
    throw err
  }
}

/** Открыть PDF, полученный как Blob (предпросмотр шаблона). */
export async function openBlobInNewTab(load: () => Promise<Blob>): Promise<void> {
  await openInNewTab(async () => {
    const blob = await load()
    const url = URL.createObjectURL(blob.type ? blob : new Blob([blob], { type: 'application/pdf' }))
    setTimeout(() => URL.revokeObjectURL(url), 60_000)
    return url
  })
}
