import { expect, test } from '@playwright/test'

test('persists the German interface selection', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'German' }).click()

  await expect(page.getByRole('heading', { name: 'Jedes WAN in einer Betriebsübersicht.' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'de')

  await page.goto('/tasks')
  await expect(page.getByRole('heading', { name: 'Unabhängige Tests, präzise geroutet.' })).toBeVisible()
})
