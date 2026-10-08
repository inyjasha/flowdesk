import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, ArrowUpRight, Check, CheckCheck, ChevronDown, ChevronLeft, ChevronRight, Circle, CircleCheck, Clock3, Flag, LayoutDashboard, Leaf, ListChecks, LoaderCircle, LogOut, Plus, Search, Settings2, Sparkles, Users, X } from 'lucide-react'
import { api, APIError, errorText } from './api'
import { columns, progress, visibleTasks } from './board'
import type { Activity, Member, Priority, Role, Status, Task, TaskInput, TaskPage, User, Workspace } from './types'

const roles: Record<Role, string> = { owner: 'Владелец', editor: 'Редактор', viewer: 'Наблюдатель' }
const priorityLabels: Record<Priority, string> = { high: 'Высокий', medium: 'Средний', low: 'Низкий' }
const statusLabels = Object.fromEntries(columns.map(c => [c.status, c.label]))
const date = (value: string) => new Date(value).toLocaleDateString('ru', { day: 'numeric', month: 'short' })
const initials = (name: string) => name.split(' ').map(p => p[0]).slice(0, 2).join('').toUpperCase()

function Modal({ title, children, close }: { title: string; children: ReactNode; close: () => void }) {
  const ref = useRef<HTMLDialogElement>(null)
  const titleID = useId()
  useEffect(() => { ref.current?.showModal(); const dialog = ref.current; return () => { dialog?.close() } }, [])
  return <dialog ref={ref} className="modal" aria-labelledby={titleID} onCancel={close} onClick={e => { if (e.target === e.currentTarget) close() }}>
    <div className="modal-head"><h2 id={titleID}>{title}</h2><button className="icon-button" aria-label="Закрыть" onClick={close}><X size={20} /></button></div>{children}
  </dialog>
}

function AuthScreen({ onDone }: { onDone: (user: User) => void }) {
  const [register, setRegister] = useState(false)
  const [error, setError] = useState('')
  const auth = useMutation({ mutationFn: (data: Record<string, string>) => api<User>('/auth/' + (register ? 'register' : 'login'), { method: 'POST', body: JSON.stringify(data) }), onSuccess: onDone, onError: e => setError(errorText(e)) })
  function submit(e: FormEvent<HTMLFormElement>) { e.preventDefault(); setError(''); auth.mutate(Object.fromEntries(new FormData(e.currentTarget)) as Record<string, string>) }
  return <div className="auth-layout">
    <section className="auth-story"><div className="brand"><Leaf size={27} /> flowdesk<span className="brand-dot">.</span></div>
      <div className="story-copy"><span className="eyebrow">МЕНЬШЕ ШУМА. БОЛЬШЕ ЯСНОСТИ.</span><h1>Хорошие идеи<br />нуждаются<br />в пространстве<span>.</span></h1><p>Планируйте вместе, держите главное в фокусе<br />и замечайте, как работа движется вперёд.</p>
        <div className="story-preview"><div><span className="preview-dot" /> Команда в одном ритме</div><div className="preview-card"><CheckCheck size={22} /><span>От идеи до результата<small>Каждый шаг на своём месте</small></span><ArrowUpRight size={20} /></div></div>
      </div><p className="story-footer">Небольшие шаги. Общий результат.</p>
    </section>
    <section className="auth-form"><div><span className="eyebrow">ВАШЕ РАБОЧЕЕ ПРОСТРАНСТВО</span><h2>{register ? 'Начнём с знакомства' : 'С возвращением'}</h2><p className="muted">{register ? 'Создайте аккаунт и пригласите свою команду.' : 'Войдите, чтобы продолжить с того же места.'}</p>
      <form onSubmit={submit}>
        {register && <label>Как вас зовут<input name="name" required maxLength={50} placeholder="Анна" autoComplete="name" /></label>}
        <label>Email<input name="email" type="email" required placeholder="anna@example.com" autoComplete="email" /></label>
        <label>Пароль<input name="password" type="password" required minLength={register ? 10 : 1} maxLength={72} autoComplete={register ? 'new-password' : 'current-password'} placeholder={register ? 'Не меньше 10 символов' : 'Ваш пароль'} /></label>
        {error && <p role="alert" className="error">{error}</p>}
        <button className="button primary full" disabled={auth.isPending}>{auth.isPending ? <LoaderCircle size={18} className="spin" /> : <>{register ? 'Создать аккаунт' : 'Войти в пространство'}<ArrowRight size={18} /></>}</button>
      </form>
      <p className="auth-switch">{register ? 'Уже есть аккаунт?' : 'Первый раз здесь?'} <button onClick={() => { setRegister(!register); setError('') }}>{register ? 'Войти' : 'Создать аккаунт'}</button></p>
    </div></section>
  </div>
}

export default function App() {
  const qc = useQueryClient()
  const me = useQuery({ queryKey: ['me'], queryFn: () => api<User>('/me') })
  if (me.isPending) return <div className="screen-loading"><Leaf size={36} /><p>Открываем пространство…</p></div>
  if (me.error) {
    if (me.error instanceof APIError && me.error.status === 401) return <AuthScreen onDone={user => { qc.removeQueries({ predicate: q => q.queryKey[0] !== 'me' }); qc.setQueryData(['me'], user) }} />
    return <div className="screen-loading"><p className="error">{errorText(me.error)}</p><button className="button" onClick={() => me.refetch()}>Попробовать снова</button></div>
  }
  return <Dashboard user={me.data!} />
}

function Dashboard({ user }: { user: User }) {
  const qc = useQueryClient()
  const spaces = useQuery({ queryKey: ['workspaces'], queryFn: () => api<Workspace[]>('/workspaces') })
  const [workspaceID, setWorkspaceID] = useState<number | null>(null)
  const [section, setSection] = useState<'board' | 'team' | 'activity'>('board')
  const [modal, setModal] = useState<'create' | 'join' | null>(null)
  const [error, setError] = useState('')
  const active = spaces.data?.find(w => w.id === workspaceID) || spaces.data?.[0]
  const create = useMutation({ mutationFn: (name: string) => api<Workspace>('/workspaces', { method: 'POST', body: JSON.stringify({ name }) }), onSuccess: w => { qc.invalidateQueries({ queryKey: ['workspaces'] }); setWorkspaceID(w.id); setModal(null) }, onError: e => setError(errorText(e)) })
  const join = useMutation({ mutationFn: (token: string) => api<Workspace>('/invites/accept', { method: 'POST', body: JSON.stringify({ token }) }), onSuccess: w => { qc.invalidateQueries({ queryKey: ['workspaces'] }); setWorkspaceID(w.id); setModal(null) }, onError: e => setError(errorText(e)) })
  const logout = useMutation({ mutationFn: () => api<void>('/auth/logout', { method: 'POST' }), onSuccess: () => { qc.removeQueries({ predicate: q => q.queryKey[0] !== 'me' }); qc.resetQueries({ queryKey: ['me'] }) }, onError: e => setError(errorText(e)) })
  const open = (kind: 'create' | 'join') => { setError(''); setModal(kind) }
  return <div className="app-shell">
    <aside className="sidebar"><div className="brand"><Leaf size={26} /> flowdesk<span className="brand-dot">.</span></div>
      <div className="workspace-picker"><div className="workspace-mark">{initials(active?.name || 'FD')}</div><div><small>ПРОСТРАНСТВО</small><select aria-label="Пространство" value={active?.id || ''} onChange={e => { setWorkspaceID(Number(e.target.value)); setSection('board') }}>
        {!spaces.data?.length && <option value="">Ваша команда</option>}{spaces.data?.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
      </select></div><ChevronDown size={14} /></div>
      <span className="nav-label">РАБОТА КОМАНДЫ</span><nav>
        <button className={section === 'board' ? 'active' : ''} onClick={() => setSection('board')}><LayoutDashboard size={18} /> Доска задач<span className="nav-arrow">↗</span></button>
        <button className={section === 'team' ? 'active' : ''} onClick={() => setSection('team')}><Users size={18} /> Команда</button>
        <button className={section === 'activity' ? 'active' : ''} onClick={() => setSection('activity')}><Clock3 size={18} /> История изменений</button>
      </nav><div className="space-actions"><button onClick={() => open('create')}><Plus size={16} /> Новое пространство</button><button onClick={() => open('join')}><ArrowUpRight size={16} /> Войти по приглашению</button></div>
      <div className="sidebar-note"><Sparkles size={19} /><h3>Один шаг за раз</h3><p>Большие результаты начинаются с маленьких завершённых задач.</p><span>Держите свой ритм ↗</span></div>
      <div className="user-card"><span className="avatar">{initials(user.name)}</span><div><strong>{user.name}</strong><small>{user.email}</small></div><button className="icon-button" title="Выйти" aria-label="Выйти" disabled={logout.isPending} onClick={() => logout.mutate()}><LogOut size={17} /></button></div>
    </aside>
    <main className="main"><header className="topbar"><span><span className="muted">Пространство</span><ChevronRight size={14} />{active?.name || 'Начало работы'}</span><span className="topbar-right"><span className="small-dot" /> Рабочий ритм</span></header>
      {error && !modal && <div className="error page-error" role="alert">{error}</div>}
      {spaces.isPending ? <div className="screen-loading">Загружаем пространства…</div> : spaces.error ? <div className="empty"><p className="error">{errorText(spaces.error)}</p><button className="button" onClick={() => spaces.refetch()}>Повторить</button></div> : !active ? <div className="welcome"><div className="welcome-icon"><Leaf size={38} /></div><span className="eyebrow">НАЧАЛО ЧЕГО-ТО ХОРОШЕГО</span><h1>Здесь будет ваша команда</h1><p>Создайте первое пространство, добавьте задачи<br />и пригласите тех, с кем двигаетесь к результату.</p><div><button className="button primary" onClick={() => open('create')}><Plus size={18} />Создать пространство</button><button className="button" onClick={() => open('join')}>У меня есть приглашение</button></div></div> : <WorkspaceView key={active.id} workspace={active} section={section} />}
    </main>
    {modal && <Modal title={modal === 'create' ? 'Новое пространство' : 'Присоединиться к команде'} close={() => setModal(null)}><form onSubmit={e => { e.preventDefault(); setError(''); const value = String(new FormData(e.currentTarget).get('value')); modal === 'create' ? create.mutate(value) : join.mutate(value.trim()) }}>
      <p className="muted">{modal === 'create' ? 'Назовите его так, чтобы команда узнала своё место.' : 'Вставьте одноразовый код, который прислал владелец пространства.'}</p>
      <label>{modal === 'create' ? 'Название' : 'Код приглашения'}<input name="value" required maxLength={modal === 'create' ? 50 : 64} autoFocus placeholder={modal === 'create' ? 'Например, Product team' : 'Код из приглашения'} /></label>
      {error && <p className="error" role="alert">{error}</p>}<button className="button primary full" disabled={create.isPending || join.isPending}>{modal === 'create' ? 'Создать пространство' : 'Присоединиться'}<ArrowRight size={17} /></button>
    </form></Modal>}
  </div>
}

function WorkspaceView({ workspace, section }: { workspace: Workspace; section: 'board' | 'team' | 'activity' }) {
  const qc = useQueryClient()
  const [live, setLive] = useState(false)
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const [offset, setOffset] = useState(0)
  const [priority, setPriority] = useState('')
  const [assignee, setAssignee] = useState('')
  const [editing, setEditing] = useState<Task | 'new' | null>(null)
  const [newStatus, setNewStatus] = useState<Status>('todo')
  const [error, setError] = useState('')
  const prefix = `/workspaces/${workspace.id}`
  useEffect(() => { const timer = setTimeout(() => { setQuery(search); setOffset(0) }, 250); return () => clearTimeout(timer) }, [search])
  const tasks = useQuery({ queryKey: ['tasks', workspace.id, query, offset, priority, assignee], queryFn: () => api<TaskPage>(`${prefix}/tasks?limit=100&offset=${offset}&q=${encodeURIComponent(query)}&priority=${priority}&assignee=${assignee}`), placeholderData: previous => previous, refetchInterval: 30000 })
  const members = useQuery({ queryKey: ['members', workspace.id], queryFn: () => api<Member[]>(`${prefix}/members`) })
  const history = useQuery({ queryKey: ['activity', workspace.id], queryFn: () => api<Activity[]>(`${prefix}/activity`) })
  const editable = workspace.role !== 'viewer'
  const invalidate = () => { qc.invalidateQueries({ queryKey: ['tasks', workspace.id] }); qc.invalidateQueries({ queryKey: ['activity', workspace.id] }); qc.invalidateQueries({ queryKey: ['members', workspace.id] }) }
  useEffect(() => {
    const source = new EventSource(`${'/api' + prefix}/stream`)
    let timer: ReturnType<typeof setTimeout> | undefined
    const refresh = () => { clearTimeout(timer); timer = setTimeout(() => { qc.invalidateQueries({ queryKey: ['tasks', workspace.id] }); qc.invalidateQueries({ queryKey: ['activity', workspace.id] }); qc.invalidateQueries({ queryKey: ['members', workspace.id] }) }, 100) }
    source.addEventListener('ready', () => { setLive(true); refresh() })
    source.addEventListener('change', refresh)
    source.onerror = () => { setLive(false); qc.invalidateQueries({ queryKey: ['me'] }) }
    return () => { source.close(); clearTimeout(timer) }
  }, [workspace.id, prefix, qc])
  const save = useMutation({ mutationFn: ({ task, input }: { task: Task | 'new'; input: TaskInput }) => api<Task>(prefix + '/tasks' + (task === 'new' ? '' : `/${task.id}`), { method: task === 'new' ? 'POST' : 'PUT', body: JSON.stringify(input) }), onSuccess: () => { invalidate(); setEditing(null); setError('') }, onError: e => setError(errorText(e)) })
  const move = useMutation({ mutationFn: ({ task, status }: { task: Task; status: Status }) => api<Task>(`${prefix}/tasks/${task.id}`, { method: 'PUT', body: JSON.stringify({ ...task, status }) }), onSuccess: invalidate, onError: e => { setError(errorText(e)); invalidate() } })
  const remove = useMutation({ mutationFn: (task: Task) => api<void>(`${prefix}/tasks/${task.id}?version=${task.version}`, { method: 'DELETE' }), onSuccess: () => { invalidate(); setEditing(null); setError('') }, onError: e => setError(errorText(e)) })
  const items = tasks.data?.items || []
  const filtered = visibleTasks(items, priority, assignee)
  const open = (task: Task | 'new', status: Status = 'todo') => { setError(''); setNewStatus(status); setEditing(task) }
  return <div className="workspace-content">
    <div className="page-heading"><div><div className="heading-kicker"><span className="eyebrow">{section === 'board' ? 'ОТ ПЛАНА К РЕЗУЛЬТАТУ' : section === 'team' ? 'ЛЮДИ, С КОТОРЫМИ ВЫ РЯДОМ' : 'КАЖДЫЙ ШАГ ИМЕЕТ ЗНАЧЕНИЕ'}</span><span className={'live-badge ' + (live ? 'online' : '')}><span />{live ? 'Синхронизировано' : 'Переподключение'}</span></div><h1>{section === 'board' ? 'Всё движется вперёд' : section === 'team' ? 'Ваша команда' : 'История изменений'}<span>.</span></h1><p className="muted">{section === 'board' ? 'Общий план, понятные приоритеты и место для хороших идей.' : section === 'team' ? 'Одна команда. Разные роли. Общая цель.' : 'Что изменилось, кто помог и как продвигается работа.'}</p></div>
      {section === 'board' && editable && <button className="button primary" onClick={() => open('new')}><Plus size={18} />Новая задача</button>}
    </div>
    {section === 'board' ? <>
      <div className="stats-grid"><Stat label={query || priority || assignee ? 'Найдено задач' : 'Задач в пространстве'} value={tasks.data?.total || 0} detail={query || priority || assignee ? 'По текущим фильтрам' : 'Весь список'} icon={<ListChecks size={22} />} /><Stat label="В работе" value={items.filter(t => t.status === 'in_progress').length} detail="На текущей странице" icon={<Clock3 size={22} />} /><Stat label="Завершено" value={items.filter(t => t.status === 'done').length} detail="На текущей странице" icon={<CircleCheck size={22} />} /><div className="stat-card progress-card"><div className="stat-top"><span>Ритм команды</span><ArrowUpRight size={20} /></div><div className="progress-value">{progress(items)}<span>%</span><small>задач страницы готовы</small></div><progress max="100" value={progress(items)} /></div></div>
      <div className="board-toolbar"><div className="board-view"><LayoutDashboard size={17} />Доска<span className="count">{filtered.length}</span></div><div className="filters"><label className="search-input"><Search size={16} /><input aria-label="Поиск задач" placeholder="Найти задачу…" value={search} onChange={e => setSearch(e.target.value)} /></label><select aria-label="Приоритет" value={priority} onChange={e => setPriority(e.target.value)}><option value="">Все приоритеты</option>{Object.entries(priorityLabels).map(([key, value]) => <option key={key} value={key}>{value}</option>)}</select><select aria-label="Исполнитель" value={assignee} onChange={e => setAssignee(e.target.value)}><option value="">Все участники</option>{members.data?.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</select><Settings2 size={17} className="filter-icon" /></div></div>
      {error && !editing && <p className="error" role="alert">{error}</p>}
      {tasks.error ? <div className="empty"><p className="error">{errorText(tasks.error)}</p><button className="button" onClick={() => tasks.refetch()}>Повторить</button></div> : tasks.isPending ? <div className="empty"><LoaderCircle className="spin" /> Загружаем задачи…</div> : <div className="board">{columns.map(column => <section className={'board-column column-' + column.status} key={column.status} onDragOver={e => { if (editable) e.preventDefault() }} onDrop={e => { e.preventDefault(); const task = items.find(t => t.id === Number(e.dataTransfer.getData('text/plain'))); if (editable && task && task.status !== column.status && !move.isPending) move.mutate({ task, status: column.status }) }}>
        <div className="column-heading"><span className="status-symbol">{column.status === 'done' ? <Check size={15} /> : column.status === 'in_progress' ? <Clock3 size={15} /> : <Circle size={15} />}</span><h2>{column.label}</h2><span className="count">{filtered.filter(t => t.status === column.status).length}</span></div><p className="column-caption">{column.caption}</p>
        <div className="column-tasks">{filtered.filter(t => t.status === column.status).map(task => <TaskCard key={task.id} task={task} member={members.data?.find(m => m.id === task.assignee_id)} editable={editable} open={() => open(task)} />)}
        {!filtered.some(t => t.status === column.status) && <div className="column-empty">{query || priority || assignee ? 'Подходящих задач нет' : 'Здесь пока спокойно'}<small>{editable ? 'Добавьте задачу или перенесите её сюда' : 'Новые задачи появятся здесь'}</small></div>}</div>
        {editable && <button className="add-task" onClick={() => open('new', column.status)}><Plus size={15} />Добавить задачу</button>}
      </section>)}</div>}
      <footer className="board-footer"><span><span className="small-dot" />{editable ? 'Перетаскивайте карточки или меняйте статус внутри задачи' : 'Режим наблюдателя · задачи доступны для просмотра'}</span>{(tasks.data?.total || 0) > 100 && <div className="pagination"><button aria-label="Предыдущая страница" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 100))}><ChevronLeft size={17} /></button><span>{offset + 1}–{offset + items.length} из {tasks.data?.total}</span><button aria-label="Следующая страница" disabled={offset + 100 >= (tasks.data?.total || 0)} onClick={() => setOffset(offset + 100)}><ChevronRight size={17} /></button></div>}</footer>
    </> : section === 'team' ? <Team workspace={workspace} members={members.data || []} error={members.error} /> : <div className="activity-panel">{history.error && <p className="error">{errorText(history.error)}</p>}{history.data?.length === 0 && <div className="empty">История начнётся с первой задачи.</div>}{history.data?.map(a => <div className="activity-row" key={a.id}><span className="activity-icon">{a.action === 'created' ? <Plus size={17} /> : a.action === 'deleted' ? <X size={17} /> : <Check size={17} />}</span><div><strong>{a.actor}</strong> <span className="muted">{a.action === 'created' ? 'создал(а)' : a.action === 'deleted' ? 'удалил(а)' : 'изменил(а)'} задачу</span><p>{a.title}</p></div><time>{new Date(a.created_at).toLocaleString('ru', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })}</time></div>)}</div>}
    {editing && <TaskEditor task={editing} defaultStatus={newStatus} members={members.data || []} editable={editable} pending={save.isPending || remove.isPending} error={error} close={() => setEditing(null)} save={input => save.mutate({ task: editing, input })} remove={() => { if (editing !== 'new') remove.mutate(editing) }} />}
  </div>
}

function Stat({ label, value, detail, icon }: { label: string; value: number; detail: string; icon: ReactNode }) { return <div className="stat-card"><div className="stat-top"><span>{label}</span><span className="stat-icon">{icon}</span></div><strong>{value.toString().padStart(2, '0')}</strong><small>{detail}</small></div> }
function TaskCard({ task, member, editable, open }: { task: Task; member?: Member; editable: boolean; open: () => void }) {
  const overdue = task.due_date && new Date(task.due_date) < new Date() && task.status !== 'done'
  return <article className={'task-card ' + (task.status === 'done' ? 'completed' : '')} draggable={editable} onDragStart={e => e.dataTransfer.setData('text/plain', String(task.id))}>
    <div className="card-top"><span className={'priority priority-' + task.priority}><Flag size={11} />{priorityLabels[task.priority]}</span><span className="task-number">FD-{String(task.id).padStart(3, '0')}</span></div>
    <button className="task-title" onClick={open}>{task.title}</button>{task.description && <p>{task.description}</p>}
    <div className="card-footer"><span className={'due-date ' + (overdue ? 'overdue' : '')}>{task.due_date ? <><Clock3 size={13} />{date(task.due_date)}</> : <span className="no-date">Без срока</span>}</span>{member ? <span className="avatar small" title={member.name}>{initials(member.name)}</span> : <span className="unassigned" title="Без исполнителя"><Users size={15} /></span>}</div>
  </article>
}
function TaskEditor({ task, defaultStatus, members, editable, pending, error, close, save, remove }: { task: Task | 'new'; defaultStatus: Status; members: Member[]; editable: boolean; pending: boolean; error: string; close: () => void; save: (input: TaskInput) => void; remove: () => void }) {
  const [deleting, setDeleting] = useState(false)
  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault(); const f = new FormData(e.currentTarget); const due = String(f.get('due_date'))
    save({ title: String(f.get('title')), description: String(f.get('description')), status: String(f.get('status')) as Status, priority: String(f.get('priority')) as Priority, assignee_id: f.get('assignee_id') ? Number(f.get('assignee_id')) : null, due_date: due ? `${due}T23:59:59Z` : null, version: task === 'new' ? 0 : task.version })
  }
  const data = task === 'new' ? null : task
  return <Modal title={data ? `Задача FD-${String(data.id).padStart(3, '0')}` : 'Новая задача'} close={close}>
    <form onSubmit={submit}><fieldset disabled={!editable || pending}>
      <label>Название<input name="title" required maxLength={100} defaultValue={data?.title} autoFocus placeholder="Что нужно сделать?" /></label>
      <label>Описание<textarea name="description" rows={4} maxLength={2500} defaultValue={data?.description} placeholder="Контекст, детали и ожидаемый результат" /></label>
      <div className="form-grid"><label>Статус<select name="status" defaultValue={data?.status || defaultStatus}>{columns.map(c => <option key={c.status} value={c.status}>{c.label}</option>)}</select></label><label>Приоритет<select name="priority" defaultValue={data?.priority || 'medium'}>{Object.entries(priorityLabels).map(([key, value]) => <option key={key} value={key}>{value}</option>)}</select></label><label>Исполнитель<select name="assignee_id" defaultValue={data?.assignee_id || ''}><option value="">Не назначен</option>{members.filter(m => m.role !== 'viewer').map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</select></label><label>Срок<input type="date" name="due_date" defaultValue={data?.due_date?.slice(0, 10) || ''} /></label></div>
    </fieldset>{error && <p role="alert" className="error">{error}</p>}{editable && <div className="modal-actions">{data && <button type="button" className="button danger" disabled={pending} onClick={() => deleting ? remove() : setDeleting(true)}>{deleting ? 'Подтвердить удаление' : 'Удалить'}</button>}<button className="button primary" disabled={pending}>{pending ? 'Сохраняем…' : 'Сохранить задачу'}<Check size={17} /></button></div>}
    </form>{!editable && <p className="muted">Вы можете просматривать задачи. Редактирование доступно владельцу и редакторам.</p>}
  </Modal>
}
function Team({ workspace, members, error }: { workspace: Workspace; members: Member[]; error: unknown }) {
  const [role, setRole] = useState<Role>('editor')
  const [token, setToken] = useState('')
  const [copied, setCopied] = useState(false)
  const invite = useMutation({ mutationFn: () => api<{ token: string }>(`/workspaces/${workspace.id}/invites`, { method: 'POST', body: JSON.stringify({ role }) }), onSuccess: data => { setToken(data.token); setCopied(false) } })
  return <div className="team-grid"><section className="team-panel"><div className="panel-title"><h2>Участники пространства</h2><span className="count">{members.length}</span></div>{error ? <p className="error">{errorText(error)}</p> : members.map(m => <div className="member-row" key={m.id}><span className="avatar">{initials(m.name)}</span><div><strong>{m.name}</strong><small>{m.email}</small></div><span className={'role role-' + m.role}>{roles[m.role]}</span></div>)}</section><section className="invite-panel"><Users size={28} /><h2>Хорошая работа —<br />командная работа</h2><p className="muted">Редактор может работать с задачами. Наблюдатель — читать доску и историю.</p>{workspace.role === 'owner' ? <><label>Роль нового участника<select value={role} onChange={e => setRole(e.target.value as Role)}><option value="editor">Редактор</option><option value="viewer">Наблюдатель</option></select></label><button className="button primary full" disabled={invite.isPending} onClick={() => invite.mutate()}><Plus size={17} />Создать приглашение</button>{invite.error && <p className="error">{errorText(invite.error)}</p>}{token && <div className="invite-result"><label>Одноразовый код<input readOnly value={token} aria-label="Код приглашения" /></label><button className="button full" onClick={async () => { try { await navigator.clipboard.writeText(token); setCopied(true) } catch { setCopied(false) } }}>{copied ? 'Скопировано' : 'Скопировать код'}</button><small>Действует 24 часа. Передайте код участнику: после регистрации он сможет выбрать «Войти по приглашению».</small></div>}</> : <p className="muted">Приглашать участников может владелец пространства.</p>}</section></div>
}
