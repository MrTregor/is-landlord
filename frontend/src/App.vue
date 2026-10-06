<script setup lang="ts">
// Корень приложения: раскладка по meta.layout маршрута, уведомления и подтверждения.
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AuthLayout from '@/layouts/AuthLayout.vue'
import CabinetLayout from '@/layouts/CabinetLayout.vue'
import OwnerLayout from '@/layouts/OwnerLayout.vue'

const route = useRoute()
const layout = computed(() => {
  switch (route.meta.layout) {
    case 'auth':
      return AuthLayout
    case 'owner':
      return OwnerLayout
    case 'cabinet':
      return CabinetLayout
    default:
      return null
  }
})
</script>

<template>
  <component :is="layout" v-if="layout">
    <RouterView />
  </component>
  <RouterView v-else />
  <Toast />
  <ConfirmDialog />
</template>
