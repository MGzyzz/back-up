import type { EnvGroup } from '../lib/group'
import { JobRow } from './JobRow'

export function EnvSection({ group }: { group: EnvGroup }) {
  const allOk = group.ok === group.total

  return (
    <section className="mb-8">
      <header className="mb-2 flex items-baseline justify-between border-b border-gray-200 pb-1 dark:border-gray-700">
        <h2 className="text-lg font-semibold">{group.environment}</h2>
        <span className={allOk ? 'text-sm text-gray-500 dark:text-gray-400' : 'text-sm font-semibold text-red-700 dark:text-red-400'}>
          {group.ok} / {group.total}
        </span>
      </header>

      <div className="overflow-x-auto">
        <table className="w-full min-w-[784px] table-fixed text-sm">
          {/* Одинаковая сетка во всех секциях не даёт колонке нод прыгать. */}
          <colgroup>
            <col className="w-48" />
            <col className="w-24" />
            <col className="w-20" />
            <col className="w-20" />
            <col className="w-32" />
            <col className="w-52" />
          </colgroup>
          <thead>
            <tr className="text-left text-xs uppercase tracking-wide text-gray-400 dark:text-gray-500">
              <th className="pb-1 pr-4 font-normal">Бэкап</th>
              <th className="pb-1 pr-4 font-normal">Статус</th>
              <th className="pb-1 pr-4 font-normal">Начало</th>
              <th className="pb-1 pr-4 font-normal">Конец</th>
              <th className="pb-1 pr-4 font-normal">Длительность</th>
              <th className="pb-1 font-normal">Ноды</th>
            </tr>
          </thead>
          <tbody>
            {group.jobs.map((job) => (
              <JobRow key={`${job.environment}/${job.label}`} job={job} />
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
