<script setup lang="ts">
// Метка статуса: всегда с текстом (статус не передаётся только цветом), цвет — по таблице 13 Фронтенд, раздел 6.
import type { Severity } from '@/utils/labels'

defineProps<{ label: string; severity?: Severity }>()
</script>

<template>
  <Tag :value="label" :severity="severity ?? 'secondary'" />
</template>
