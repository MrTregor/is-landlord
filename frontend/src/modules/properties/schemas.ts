import { z } from 'zod'

const text = (max: number) => z.string().trim().max(max)

/** Форма объекта (14 Экраны собственника, раздел 2.2). */
export const propertySchema = z
  .object({
    code: z
      .string()
      .trim()
      .min(2, 'Короткое название — от 2 до 40 символов')
      .max(40, 'Короткое название — от 2 до 40 символов'),
    kind: z.enum(['residential', 'non_residential']),
    address: z.string().trim().min(1, 'Укажите адрес').max(300, 'Не более 300 символов'),
    areaSqm: z
      .string()
      .trim()
      .regex(/^\d{1,5}([.,]\d{1,2})?$/, 'Площадь — число, не более двух знаков после запятой')
      .refine((v) => Number(v.replace(',', '.')) > 0, 'Площадь должна быть больше нуля'),
    floor: z.number().int().min(-5, 'Этаж — от −5 до 200').max(200, 'Этаж — от −5 до 200').nullable(),
    rooms: z.number().int().min(0, 'Не меньше 0').nullable(),
    cadastralNumber: text(40).refine(
      (v) => v === '' || /^\d{2}:\d{2}:\d{6,7}:\d+$/.test(v),
      'Формат: 50:19:0010101:123',
    ),
    ownershipBasis: text(500),
    furnished: z.boolean(),
    description: text(4000),
    expectedRentKop: z.number().int().min(0, 'Не может быть отрицательной'),
    fiscalMode: z.enum(['none', 'cloud_kkt', 'npd_manual']),
    initialStatus: z.enum(['free', 'repair']),
    statusSince: z.string().min(1, 'Укажите дату'),
  })
  .refine((v) => v.kind === 'residential' || v.rooms === null, {
    path: ['rooms'],
    message: 'Число комнат указывается только для жилых помещений',
  })

export type PropertyForm = z.infer<typeof propertySchema>

/** Площадь для API: «24,5» → «24.50». */
export function areaToApi(v: string): string {
  const [int = '0', frac = ''] = v.trim().replace(',', '.').split('.')
  return `${int}.${frac.padEnd(2, '0')}`
}

/** Площадь для вывода: «24.50» → «24,5». */
export function areaToText(v: string): string {
  return v.replace(/\.?0+$/, '').replace('.', ',')
}
