<script setup lang="ts">
// Заглушка экрана, который делается на следующем этапе плана (19 План реализации).
import { useRoute } from 'vue-router'
import PageHeader from './PageHeader.vue'

const route = useRoute()
</script>

<template>
  <div>
    <PageHeader :title="route.meta.title ?? 'Раздел'" />
    <Message severity="info" :closable="false">
      Раздел будет доступен после этапа {{ route.meta.stage }} плана реализации.
    </Message>
  </div>
</template>
