import { test, expect } from '@playwright/test'

test('регистрация, доска, редактирование и повторный вход', async ({ page }, testInfo) => {
  const email = `e2e-${Date.now()}@example.com`
  await page.goto('/')
  await page.getByRole('button', { name: 'Создать аккаунт', exact: true }).click()
  await page.getByLabel('Как вас зовут').fill('Анна Тест')
  await page.getByLabel('Email', { exact: true }).fill(email)
  await page.getByLabel('Пароль', { exact: true }).fill('test-password-123')
  await page.getByRole('button', { name: 'Создать аккаунт', exact: true }).click()
  await page.getByRole('button', { name: 'Создать пространство', exact: true }).click()
  await page.getByLabel('Название', { exact: true }).fill('Тестовая команда')
  await page.getByRole('dialog').getByRole('button', { name: 'Создать пространство', exact: true }).click()
  await page.getByRole('button', { name: 'Новая задача', exact: true }).click()
  await page.getByLabel('Название', { exact: true }).fill('Проверить пользовательский сценарий')
  await page.getByLabel('Описание', { exact: true }).fill('Создание и редактирование должны работать из браузера.')
  await page.getByRole('button', { name: 'Сохранить задачу', exact: true }).click()
  await page.getByRole('button', { name: 'Проверить пользовательский сценарий', exact: true }).click()
  await page.getByRole('dialog').getByRole('combobox', { name: 'Статус', exact: true }).selectOption('done')
  await page.getByRole('button', { name: 'Сохранить задачу', exact: true }).click()
  await expect(page.locator('.column-done')).toContainText('Проверить пользовательский сценарий')
  await page.screenshot({ path: testInfo.outputPath('board.png'), fullPage: true })
  await page.getByRole('button', { name: 'История изменений', exact: true }).click()
  await expect(page.locator('.activity-panel')).toContainText('изменил(а)')
  await page.getByRole('button', { name: 'Выйти', exact: true }).click()
  await page.getByLabel('Email', { exact: true }).fill(email)
  await page.getByLabel('Пароль', { exact: true }).fill('test-password-123')
  await page.getByRole('button', { name: 'Войти в пространство', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Новая задача', exact: true })).toBeVisible()
})

test('наблюдатель видит изменения второй вкладки и не может редактировать', async ({ browser }) => {
  const ownerContext = await browser.newContext()
  const viewerContext = await browser.newContext()
  try {
    const headers = { 'X-Flowdesk-Request': '1' }
    const base = process.env.BASE_URL || 'http://localhost:8090'
    const register = async (context: typeof ownerContext, name: string) => {
      const response = await context.request.post(base + '/api/auth/register', { headers, data: { name, email: `${name}-${Date.now()}@example.com`, password: 'test-password-123' } })
      expect(response.status()).toBe(201)
    }
    await register(ownerContext, 'owner')
    await register(viewerContext, 'viewer')
    const created = await ownerContext.request.post(base + '/api/workspaces', { headers, data: { name: 'Общая доска' } })
    expect(created.status()).toBe(201)
    const workspace = await created.json()
    const invite = await ownerContext.request.post(`${base}/api/workspaces/${workspace.id}/invites`, { headers, data: { role: 'viewer' } })
    const { token } = await invite.json()
    const joined = await viewerContext.request.post(base + '/api/invites/accept', { headers, data: { token } })
    expect(joined.status()).toBe(200)
    const page = await viewerContext.newPage()
    await page.goto(base)
    await expect(page.getByText('Синхронизировано', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Новая задача', exact: true })).toHaveCount(0)
    const input = { title: 'Изменение из другой вкладки', description: '', status: 'todo', priority: 'medium', assignee_id: null, due_date: null, version: 0 }
    const forbidden = await viewerContext.request.post(`${base}/api/workspaces/${workspace.id}/tasks`, { headers, data: input })
    expect(forbidden.status()).toBe(403)
    const saved = await ownerContext.request.post(`${base}/api/workspaces/${workspace.id}/tasks`, { headers, data: input })
    expect(saved.status()).toBe(201)
    await expect(page.getByRole('button', { name: input.title, exact: true })).toBeVisible()
    const task = await saved.json()
    const updated = await ownerContext.request.put(`${base}/api/workspaces/${workspace.id}/tasks/${task.id}`, { headers, data: { ...input, version: 1, status: 'done' } })
    expect(updated.status()).toBe(200)
    const stale = await ownerContext.request.put(`${base}/api/workspaces/${workspace.id}/tasks/${task.id}`, { headers, data: { ...input, version: 1, status: 'in_progress' } })
    expect(stale.status()).toBe(409)
    await expect(page.locator('.column-done')).toContainText(input.title)
  } finally { await ownerContext.close(); await viewerContext.close() }
})
