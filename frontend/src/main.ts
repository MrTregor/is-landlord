import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { VueQueryPlugin } from '@tanstack/vue-query'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import ConfirmationService from 'primevue/confirmationservice'
import 'primeicons/primeicons.css'
import './styles/tokens.css'
import './styles/base.css'
import App from './App.vue'
import { LandlordPreset, ruLocale } from './app/primevue'
import { createAppRouter } from './app/router'
import { ApiError } from './utils/problem'

const app = createApp(App)
app.use(createPinia())
app.use(VueQueryPlugin, {
  queryClientConfig: {
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        refetchOnWindowFocus: false,
        // Ответы 4xx (нет доступа, не найдено) повторять бессмысленно; сбои сети и 5xx — до 3 раз.
        retry: (count, err) => count < 3 && !(err instanceof ApiError && err.status >= 400 && err.status < 500),
      },
    },
  },
})
app.use(PrimeVue, {
  theme: { preset: LandlordPreset, options: { darkModeSelector: false } },
  locale: ruLocale,
  ripple: false,
})
app.use(ToastService)
app.use(ConfirmationService)
app.use(createAppRouter())
app.mount('#app')
