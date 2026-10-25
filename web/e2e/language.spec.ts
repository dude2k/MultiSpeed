import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'

test('persists the German interface selection', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'German' }).click()

  await expect(page.getByRole('heading', { name: 'Jedes WAN in einer Betriebsübersicht.' })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'de')

  await page.goto('/tasks')
  await expect(page.getByRole('heading', { name: 'Unabhängige Tests, präzise geroutet.' })).toBeVisible()

  const scan = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
    .analyze()
  expect(scan.violations).toEqual([])
})
