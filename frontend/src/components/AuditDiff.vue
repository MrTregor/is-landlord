<script setup lang="ts">
// «До/после» записи журнала в две колонки с подсветкой изменённых полей (13 Фронтенд, раздел 7).
import { computed } from 'vue'

const props = defineProps<{
  before: Record<string, unknown> | null
  after: Record<string, unknown> | null
}>()

function show(v: unknown): string {
  if (v === undefined) return '—'
  if (v === null) return 'пусто'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

const rows = computed(() => {
  const keys = new Set([...Object.keys(props.before ?? {}), ...Object.keys(props.after ?? {})])
  return [...keys].sort().map((key) => {
    const b = props.before ? show(props.before[key]) : '—'
    const a = props.after ? show(props.after[key]) : '—'
    return { key, before: b, after: a, changed: props.before !== null && props.after !== null && a !== b }
  })
})
</script>

<template>
  <p v-if="!rows.length" class="audit-diff__empty">Подробностей нет</p>
  <table v-else class="audit-diff">
    <thead>
      <tr>
        <th scope="col">Поле</th>
        <th scope="col">До</th>
        <th scope="col">После</th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="r in rows" :key="r.key" :class="{ 'audit-diff__changed': r.changed }">
        <th scope="row">{{ r.key }}<span v-if="r.changed" class="sr-only"> (изменено)</span></th>
        <td>{{ r.before }}</td>
        <td>{{ r.after }}</td>
      </tr>
    </tbody>
  </table>
</template>

<style scoped>
.audit-diff {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.875rem;
  table-layout: fixed;
}
.audit-diff th,
.audit-diff td {
  padding: 0.25rem 0.5rem;
  border-bottom: 1px solid var(--p-content-border-color);
  text-align: left;
  vertical-align: top;
  word-break: break-word;
}
.audit-diff thead th:first-child {
  width: 30%;
}
.audit-diff__changed td {
  background: #fef3c7;
}
.audit-diff__changed th::before {
  content: '● ';
  color: #b45309;
}
.audit-diff__empty {
  color: var(--p-text-muted-color);
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
}
</style>
