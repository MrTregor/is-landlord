import pluginVue from 'eslint-plugin-vue'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'
import skipFormatting from '@vue/eslint-config-prettier/skip-formatting'

export default defineConfigWithVueTs(
  { name: 'app/files-to-lint', files: ['**/*.{ts,mts,vue}'] },
  { name: 'app/ignores', ignores: ['dist/**', 'coverage/**', 'src/api/schema.d.ts', 'src/components.d.ts'] },
  pluginVue.configs['flat/recommended'],
  vueTsConfigs.recommended,
  skipFormatting,
  {
    rules: {
      'vue/multi-word-component-names': 'off',
      // Деньги — только целые копейки: parseFloat запрещён.
      'no-restricted-globals': ['error', { name: 'parseFloat', message: 'Деньги — целые копейки: используйте utils/money.ts' }],
      'no-restricted-properties': ['error', { object: 'Number', property: 'parseFloat', message: 'Деньги — целые копейки: используйте utils/money.ts' }],
    },
  },
)
