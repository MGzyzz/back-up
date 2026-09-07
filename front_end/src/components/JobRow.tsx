import type { Job } from '../types'
import { formatTime, formatDuration } from '../lib/format'
import { StatusBadge } from './StatusBadge'

export function JobRow({ job }: { job: Job }) {
  // У успеха показываем ноду, что отработала; у провала — всех, кто отчитался:
  // у проваленной задачи это единственный след для разбора.
  const nodes = job.ok ? job.node : job.nodes.join(', ')

  // Провал красит строку целиком, а не один бейдж: страницу читают взглядом
  // сверху вниз по десяткам строк, и пятно заметно раньше, чем текст в колонке.
  const rowCls = job.ok
    ? 'border-gray-100 dark:border-gray-800'
    : 'border-red-200 bg-red-50 dark:border-red-900/60 dark:bg-red-950/40'

  // Приглушённые колонки на красном фоне уводим в красный же: серый по розовому
  // выглядит грязью, а строка должна читаться одним пятном.
  const mutedCls = job.ok
    ? 'text-gray-600 dark:text-gray-400'
    : 'text-red-700 dark:text-red-300'
  const nodesCls = job.ok
    ? 'text-gray-500 dark:text-gray-400'
    : 'text-red-700 dark:text-red-300'

  return (
    <tr className={`border-t ${rowCls}`}>
      <td className={`py-2 pr-4 font-medium ${job.ok ? '' : 'text-red-900 dark:text-red-100'}`}>{job.name}</td>
      <td className="py-2 pr-4"><StatusBadge ok={job.ok} /></td>
      <td className={`py-2 pr-4 tabular-nums ${mutedCls}`}>{formatTime(job.start)}</td>
      <td className={`py-2 pr-4 tabular-nums ${mutedCls}`}>{formatTime(job.end)}</td>
      <td className={`py-2 pr-4 tabular-nums ${mutedCls}`}>{formatDuration(job.duration_seconds)}</td>
      <td className={`py-2 ${nodesCls}`}>{nodes || '—'}</td>
    </tr>
  )
}
