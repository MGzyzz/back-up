/** День в часовом поясе браузера, без преобразования полуночи в UTC. */
export function todayISO(now = new Date()): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}
