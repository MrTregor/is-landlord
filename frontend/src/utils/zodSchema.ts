// Адаптер схем zod 4 для vee-validate 4.
// Пакет @vee-validate/zod 4.15 поддерживает только zod 3, а стек фиксирует zod 4 (docs/open-questions.md).
import type { TypedSchema, TypedSchemaError } from 'vee-validate'
import type { z } from 'zod'

export function toTypedSchema<T extends z.ZodType>(schema: T): TypedSchema<z.input<T>, z.output<T>> {
  return {
    __type: 'VVTypedSchema',
    async parse(values) {
      const result = await schema.safeParseAsync(values)
      if (result.success) return { value: result.data, errors: [] }
      const byPath = new Map<string, string[]>()
      for (const issue of result.error.issues) {
        const path = issue.path.map(String).join('.')
        byPath.set(path, [...(byPath.get(path) ?? []), issue.message])
      }
      const errors: TypedSchemaError[] = [...byPath].map(([path, messages]) => ({ path, errors: messages }))
      return { errors }
    },
  }
}
