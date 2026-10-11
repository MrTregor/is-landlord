// Расширения TipTap для шаблонов документов (09 Документы и шаблоны, разд. 3): редактор сохраняет классы
// базовой разметки (`p.center`, `table.sign`) и служебные блоки `<div class="place-date">`, `<div class="sign-line">`,
// которые в TipTap иначе превратились бы в обычные абзацы.
import { Extension, Node } from '@tiptap/vue-3'

/** Атрибут class у абзацев, заголовков и таблиц. */
export const KeepClass = Extension.create({
  name: 'keepClass',
  addGlobalAttributes() {
    return [
      {
        types: ['paragraph', 'heading', 'table', 'tableRow', 'tableCell', 'tableHeader'],
        attributes: {
          class: {
            default: null,
            parseHTML: (el) => el.getAttribute('class'),
            renderHTML: (attrs) => (attrs.class ? { class: attrs.class as string } : {}),
          },
        },
      },
    ]
  },
})

function fromHtml(html: string): HTMLElement {
  const t = document.createElement('template')
  t.innerHTML = html
  return (t.content.firstElementChild as HTMLElement | null) ?? document.createElement('div')
}

/** Блок `<div …>…</div>` целиком: показывается как есть, не редактируется изнутри, удаляется как единое целое. */
export const RawBlock = Node.create({
  name: 'rawBlock',
  group: 'block',
  atom: true,
  selectable: true,
  addAttributes() {
    return { html: { default: '<div></div>' } }
  },
  parseHTML() {
    return [{ tag: 'div', getAttrs: (el) => ({ html: (el as HTMLElement).outerHTML }) }]
  },
  renderHTML({ node }) {
    return fromHtml(node.attrs.html as string)
  },
  addNodeView() {
    return ({ node }) => {
      const dom = fromHtml(node.attrs.html as string)
      dom.classList.add('rte__raw')
      dom.contentEditable = 'false'
      dom.title = 'Служебный блок шаблона: его можно удалить или перенести целиком'
      return { dom }
    }
  },
})
