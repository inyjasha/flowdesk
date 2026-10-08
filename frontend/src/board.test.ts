import { describe, expect, it } from 'vitest'
import { progress, visibleTasks } from './board'
import type { Task } from './types'

const tasks = [
  { id: 1, status: 'todo', priority: 'high', assignee_id: 2 },
  { id: 2, status: 'done', priority: 'high', assignee_id: 3 },
  { id: 3, status: 'in_progress', priority: 'low', assignee_id: null },
] as Task[]
describe('board', () => {
  it('combines filters without changing source data', () => {
    expect(visibleTasks(tasks, 'high', '2').map(t => t.id)).toEqual([1])
    expect(visibleTasks(tasks, '', '').length).toBe(3)
    expect(tasks.length).toBe(3)
  })
  it('handles an empty board and rounds progress', () => {
    expect(progress([])).toBe(0)
    expect(progress(tasks)).toBe(33)
  })
})
