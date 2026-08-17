import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'

const corePages = [
  { name: 'dashboard', path: '/' },
  { name: 'tasks', path: '/tasks' },
  { name: 'network', path: '/network' },
  { name: 'results', path: '/results' },
  { name: 'settings', path: '/settings' },
]

for (const corePage of corePages) {
  test(`${corePage.name} has no detectable WCAG A/AA violations`, async ({ page }) => {
    await page.goto(corePage.path)
    await expect(page.locator('main')).toBeVisible()

    const scan = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
      .analyze()

    if (scan.violations.length > 0) throw new Error(formatViolations(scan.violations))
  })
}

function formatViolations(violations: Array<{ id: string; impact?: string | null; help: string; nodes: Array<{ target: unknown }> }>): string {
  return violations
    .map((violation) => `${violation.impact ?? 'unknown'} ${violation.id}: ${violation.help}\n${violation.nodes.map((node) => `  ${JSON.stringify(node.target)}`).join('\n')}`)
    .join('\n\n')
}
