<script setup lang="ts">
// Редактор HTML на TipTap (13 Фронтенд, «Редактор шаблонов»): заголовки, начертание, выравнивание, списки, таблицы.
// document — шаблон документа: сохраняются классы и служебные блоки разметки; readonly — только просмотр.
import { onBeforeUnmount, watch } from 'vue'
import { EditorContent, useEditor } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import TextAlign from '@tiptap/extension-text-align'
import { Table, TableCell, TableHeader, TableRow } from '@tiptap/extension-table'
import { KeepClass, RawBlock } from './richTextExtensions'

const model = defineModel<string>({ required: true })
const props = defineProps<{ labelledBy?: string; document?: boolean; readonly?: boolean }>()

const editor = useEditor({
  content: model.value,
  editable: !props.readonly,
  extensions: [
    ...(props.document ? [KeepClass, RawBlock] : []),
    StarterKit.configure({ link: false }),
    TextAlign.configure({ types: ['heading', 'paragraph'] }),
    Table.configure({ resizable: false }),
    TableRow,
    TableHeader,
    TableCell,
  ],
  onUpdate: ({ editor: e }) => {
    model.value = e.getHTML()
  },
})

watch(
  () => props.readonly,
  (ro) => editor.value?.setEditable(!ro),
)

watch(model, (value) => {
  if (editor.value && value !== editor.value.getHTML()) editor.value.commands.setContent(value, { emitUpdate: false })
})

onBeforeUnmount(() => editor.value?.destroy())
</script>

<template>
  <div class="rte">
    <div v-if="editor && !readonly" class="rte__toolbar" role="toolbar" aria-label="Форматирование">
      <Button
        text
        size="small"
        label="З2"
        aria-label="Заголовок 2"
        :class="{ rte__on: editor.isActive('heading', { level: 2 }) }"
        @click="editor.chain().focus().toggleHeading({ level: 2 }).run()"
      />
      <Button
        text
        size="small"
        label="З3"
        aria-label="Заголовок 3"
        :class="{ rte__on: editor.isActive('heading', { level: 3 }) }"
        @click="editor.chain().focus().toggleHeading({ level: 3 }).run()"
      />
      <Button
        text
        size="small"
        label="Ж"
        aria-label="Полужирный"
        :class="{ rte__on: editor.isActive('bold') }"
        @click="editor.chain().focus().toggleBold().run()"
      />
      <Button
        text
        size="small"
        label="К"
        aria-label="Курсив"
        :class="{ rte__on: editor.isActive('italic') }"
        @click="editor.chain().focus().toggleItalic().run()"
      />
      <Button
        text
        size="small"
        label="Ч"
        aria-label="Подчёркнутый"
        :class="{ rte__on: editor.isActive('underline') }"
        @click="editor.chain().focus().toggleUnderline().run()"
      />
      <Button
        text
        size="small"
        icon="pi pi-align-left"
        aria-label="По левому краю"
        @click="editor.chain().focus().setTextAlign('left').run()"
      />
      <Button
        text
        size="small"
        icon="pi pi-align-center"
        aria-label="По центру"
        @click="editor.chain().focus().setTextAlign('center').run()"
      />
      <Button
        text
        size="small"
        icon="pi pi-align-justify"
        aria-label="По ширине"
        @click="editor.chain().focus().setTextAlign('justify').run()"
      />
      <Button
        text
        size="small"
        icon="pi pi-list"
        aria-label="Маркированный список"
        @click="editor.chain().focus().toggleBulletList().run()"
      />
      <Button
        text
        size="small"
        label="1."
        aria-label="Нумерованный список"
        @click="editor.chain().focus().toggleOrderedList().run()"
      />
      <Button
        text
        size="small"
        icon="pi pi-table"
        aria-label="Вставить таблицу"
        @click="editor.chain().focus().insertTable({ rows: 2, cols: 2, withHeaderRow: false }).run()"
      />
      <slot name="toolbar" :editor="editor" />
    </div>
    <EditorContent :editor="editor" class="rte__content" :aria-labelledby="labelledBy" />
  </div>
</template>

<style scoped>
.rte {
  border: 1px solid var(--p-inputtext-border-color);
  border-radius: var(--p-border-radius-md);
  background: var(--p-content-background);
}
.rte__toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 2px;
  padding: 4px;
  border-bottom: 1px solid var(--p-content-border-color);
}
.rte__on {
  background: var(--p-highlight-background);
}
.rte__content :deep(.ProseMirror) {
  min-height: 240px;
  max-height: 60vh;
  overflow-y: auto;
  padding: 0.75rem 1rem;
  outline: none;
}
.rte__content :deep(table) {
  border-collapse: collapse;
}
.rte__content :deep(.rte__raw) {
  border: 1px dashed var(--p-content-border-color);
  border-radius: 4px;
  padding: 2px 6px;
  margin: 4px 0;
  color: var(--p-text-muted-color);
  cursor: default;
}
.rte__content :deep(.rte__raw.place-date) {
  display: flex;
  justify-content: space-between;
}
.rte__content :deep(.ProseMirror-selectednode) {
  outline: 2px solid var(--p-primary-color);
}
.rte__content :deep(td),
.rte__content :deep(th) {
  border: 1px solid var(--p-content-border-color);
  padding: 2px 6px;
}
</style>
