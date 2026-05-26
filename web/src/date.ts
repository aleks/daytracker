/** Returns today's date as YYYY-MM-DD in the browser's local timezone. */
export function localToday(): string {
  const d = new Date()
  const mm = (d.getMonth() + 1).toString().padStart(2, '0')
  const dd = d.getDate().toString().padStart(2, '0')
  return `${d.getFullYear()}-${mm}-${dd}`
}
