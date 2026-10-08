<script setup lang="ts">
// Вкладка, которая наполняется на следующем этапе плана.
defineProps<{ stage: string }>()
</script>

<template>
  <Message severity="info" :closable="false">Раздел будет доступен после этапа {{ stage }} плана реализации.</Message>
</template>
