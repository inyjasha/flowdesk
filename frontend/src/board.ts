import type { Task, Status } from './types'

export const columns: { status: Status; label: string; caption: string }[] = [
  { status: 'todo', label: 'Запланировано', caption: 'Следующие шаги' },
  { status: 'in_progress', label: 'В работе', caption: 'В фокусе команды' },
  { status: 'done', label: 'Готово', caption: 'Можно выдохнуть' },
]
export function visibleTasks(tasks: Task[], priority: string, assignee: string): Task[] {
  return tasks.filter(task => (!priority || task.priority === priority) && (!assignee || String(task.assignee_id) === assignee))
}
export function progress(tasks: Task[]): number {
  return tasks.length ? Math.round(tasks.filter(task => task.status === 'done').length / tasks.length * 100) : 0
}
