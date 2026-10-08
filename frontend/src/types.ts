export type Status = 'todo' | 'in_progress' | 'done'
export type Priority = 'low' | 'medium' | 'high'
export type Role = 'owner' | 'editor' | 'viewer'
export interface User { id: number; name: string; email: string }
export interface Workspace { id: number; name: string; role: Role }
export interface Member extends User { role: Role }
export interface Task {
  id: number; workspace_id: number; title: string; description: string
  status: Status; priority: Priority; assignee_id: number | null
  due_date: string | null; version: number; updated_at: string
}
export type TaskInput = Pick<Task, 'title' | 'description' | 'status' | 'priority' | 'assignee_id' | 'due_date' | 'version'>
export interface TaskPage { items: Task[]; total: number; limit: number; offset: number }
export interface Activity { id: number; task_id: number; actor: string; action: string; title: string; created_at: string }
