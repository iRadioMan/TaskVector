export function formatDateTime(iso: string, timezone: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
    timeZone: timezone,
  }).format(new Date(iso))
}

function calendarDay(date: Date, timezone: string): number {
  const parts = zonedParts(date, timezone)
  return Date.UTC(Number(parts.year), Number(parts.month) - 1, Number(parts.day))
}

export function formatTaskDue(iso: string, timezone: string, locale: string, now = new Date()): string {
  const due = new Date(iso)
  if (due.getTime() < now.getTime()) return locale === 'ru' ? 'Просрочено' : 'Overdue'
  const dayDifference = Math.round((calendarDay(due, timezone) - calendarDay(now, timezone)) / 86_400_000)
  if (dayDifference === 0) return locale === 'ru' ? 'Сегодня' : 'Today'
  if (dayDifference === 1) return locale === 'ru' ? 'Завтра' : 'Tomorrow'
  if (dayDifference === 2) return locale === 'ru' ? 'Послезавтра' : 'Day after tomorrow'
  if (dayDifference < 7) {
    if (locale === 'ru') {
      const weekdays = ['В воскресенье', 'В понедельник', 'Во вторник', 'В среду', 'В четверг', 'В пятницу', 'В субботу']
      const weekday = new Intl.DateTimeFormat('en', { timeZone: timezone, weekday: 'short' }).format(due)
      const index = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].indexOf(weekday)
      return weekdays[index]
    }
    const weekday = new Intl.DateTimeFormat(locale, { timeZone: timezone, weekday: 'long' }).format(due)
    return weekday
  }
  const options: Intl.DateTimeFormatOptions = { timeZone: timezone, day: 'numeric', month: 'short' }
  if (new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric' }).format(due) !== new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric' }).format(now)) options.year = 'numeric'
  return new Intl.DateTimeFormat(locale, options).format(due)
}

function zonedParts(date: Date, timezone: string) {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: timezone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(date)
  return Object.fromEntries(parts.map(part => [part.type, part.value]))
}

export function toDateTimeLocal(iso: string | null, timezone: string): string {
  if (!iso) return ''
  const parts = zonedParts(new Date(iso), timezone)
  return `${parts.year}-${parts.month}-${parts.day}T${parts.hour}:${parts.minute}`
}

export function fromDateTimeLocal(value: string, timezone: string): string | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value)
  if (!match) return null
  const [, year, month, day, hour, minute] = match
  const target = Date.UTC(Number(year), Number(month) - 1, Number(day), Number(hour), Number(minute))
  let instant = target
  for (let i = 0; i < 3; i++) {
    const parts = zonedParts(new Date(instant), timezone)
    const represented = Date.UTC(Number(parts.year), Number(parts.month) - 1, Number(parts.day), Number(parts.hour), Number(parts.minute))
    const adjustment = target - represented
    instant += adjustment
    if (adjustment === 0) break
  }
  return new Date(instant).toISOString()
}
