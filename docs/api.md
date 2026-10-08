# HTTP API

Все маршруты начинаются с `/api`. В браузере сессия передаётся автоматически в HttpOnly-cookie.
Для POST, PUT и DELETE обязателен `X-Flowdesk-Request: 1`.
Сторонний `Origin` отклоняется. CLI-клиент может работать без `Origin`.

| Метод | Маршрут | Данные и результат |
| --- | --- | --- |
| POST | `/auth/register` | `{name,email,password}` → пользователь, 201, cookie |
| POST | `/auth/login` | `{email,password}` → пользователь, 200, cookie |
| POST | `/auth/logout` | 204, cookie удаляется, сессия отзывается |
| GET | `/me` | Текущий пользователь |
| GET | `/workspaces` | Массив `{id,name,role}` для текущего пользователя |
| POST | `/workspaces` | `{name}` → новое пространство, 201 |
| GET | `/workspaces/{id}/members` | Массив `{id,name,email,role}` |
| POST | `/workspaces/{id}/invites` | `{role:"editor"}` или `{role:"viewer"}` → `{token}`, 201 |
| POST | `/invites/accept` | `{token}` → пространство |
| GET | `/workspaces/{id}/tasks` | Страница `{items,total,limit,offset}` |
| POST | `/workspaces/{id}/tasks` | Новая задача, 201 |
| PUT | `/workspaces/{id}/tasks/{task}` | Все редактируемые поля и актуальная версия → задача |
| DELETE | `/workspaces/{id}/tasks/{task}?version=2` | Удаление при совпадении версии, 204 |
| GET | `/workspaces/{id}/activity` | Массив последних 50 действий |
| GET | `/workspaces/{id}/stream` | SSE: события `ready`, `change`, `ping` |

`GET /health/live` проверяет процесс, `GET /health/ready` — PostgreSQL и Redis.

## Создание задачи

```json
{
  "title": "Подготовить релиз",
  "description": "Сверить готовые задачи с планом.",
  "status": "todo",
  "priority": "high",
  "assignee_id": null,
  "due_date": "2030-01-15T23:59:59Z",
  "version": 0
}
```

Статусы: `todo`, `in_progress`, `done`. Приоритеты: `low`, `medium`, `high`.
Исполнитель должен быть участником того же пространства.
`assignee_id` и `due_date` могут быть `null`.
При создании номер версии не учитывается; новая задача получает `version: 1`.
PUT заменяет все редактируемые поля, а не частично обновляет их.

Ответ также содержит `id`, `workspace_id`, `version` и `updated_at`.
При редактировании нужно передать полученную версию.
При несовпадении возвращается `409`, запись не меняется.

## Список задач

```text
GET /api/workspaces/1/tasks?limit=100&offset=0&q=релиз&priority=high&assignee=1
```

`limit`: 1–100, по умолчанию 100. `offset`: от 0.
`q`: поиск по подстроке названия без учёта регистра, до 200 байт.
`priority`: необязательный приоритет. `assignee`: необязательный ID исполнителя.
Пустые фильтры игнорируются. Поиск и фильтры применяются до пагинации.
Сортировка: новые ID первыми. `total` — количество задач с учётом всех фильтров.

## Валидация

- Имя пользователя: непустое, до 100 байт после удаления пробелов по краям.
- Email нормализуется в нижний регистр, до 254 байт, без имени в угловых скобках.
- Пароль при регистрации: 10–72 байта, верхняя граница связана с bcrypt.
- Название пространства: непустое, до 100 байт.
- Название задачи: непустое, до 200 байт.
- Описание: до 5000 байт.
- Размер тела запроса ограничен 16 КБ.

Ограничения в байтах важны для строк с кириллицей и emoji.

## Ошибки

```json
{"error":"недостаточно прав","request_id":"..."}
```

`400` — данные неверны. `401` — требуется вход или неверный пароль.
`403` — нет доступа к пространству, роли или запрос не прошёл CSRF-проверку.
`404` — задача или приглашение не найдены.
`409` — версия устарела, email уже занят или пользователь уже состоит в пространстве.
`429` — лимит входа/регистрации. `503` — вход или готовность временно недоступны.
`500` — внутренняя ошибка без подробностей SQL.

У всех ответов есть `X-Request-ID`. У некоторых инфраструктурных ошибок тело содержит только `error`.

## Пример с curl

```bash
curl -c cookies.txt http://localhost:8090/api/auth/register \
  -H 'Content-Type: application/json' \
  -H 'X-Flowdesk-Request: 1' \
  -d '{"name":"Анна","email":"anna@example.com","password":"example-password-123"}'

curl -b cookies.txt http://localhost:8090/api/workspaces \
  -H 'Content-Type: application/json' \
  -H 'X-Flowdesk-Request: 1' \
  -d '{"name":"Моя команда"}'

curl -b cookies.txt http://localhost:8090/api/workspaces
```

В PowerShell вызывай `curl.exe`, чтобы не попасть на alias другой команды.

## SSE

```text
event: ready
data: {}

event: change
data: {"type":"tasks.changed","task_id":1,"version":2}
```

Также публикуется `members.changed` при принятии приглашения.
У событий нет курсора для воспроизведения пропущенной истории.
Клиент должен перечитать соответствующие данные после `ready` и `change`.
`ping` поддерживает соединение. Не нужно считать его изменением задачи.
