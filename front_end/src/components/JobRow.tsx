import type { Job } from '../types'
import { formatTime, formatDuration } from '../lib/format'
import { StatusBadge } from './StatusBadge'

export function JobRow({ job }: { job: Job }) {
  // У успеха показываем ноду, что отработала; у провала — всех, кто отчитался:
  // у проваленной задачи это единственный след для разбора.
  const nodes = job.ok ? job.node : job.nodes.join(', ')

  return (
    <tr className="border-t border-gray-100">
      <td className="py-2 pr-4"><StatusBadge ok={job.ok} /></td>
      <td className="py-2 pr-4 font-medium">{job.name}</td>
      <td className="py-2 pr-4 tabular-nums text-gray-600">{formatTime(job.start)}</td>
      <td className="py-2 pr-4 tabular-nums text-gray-600">{formatTime(job.end)}</td>
      <td className="py-2 pr-4 tabular-nums text-gray-600">{formatDuration(job.duration_seconds)}</td>
      <td className="py-2 text-gray-500">{nodes || '—'}</td>
    </tr>
  )
}
