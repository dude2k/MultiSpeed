import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'
import { I18nProvider, useI18n } from './i18n'

function LanguageProbe() {
  const { language, setLanguage, t } = useI18n()
  return <><p>{t('Dashboard')}</p><p>{language}</p><button type="button" onClick={() => setLanguage('de')}>Deutsch</button></>
}

describe('I18nProvider', () => {
  afterEach(() => localStorage.clear())

  it('switches to German and persists the selected language', async () => {
    const user = userEvent.setup()
    render(<I18nProvider><LanguageProbe /></I18nProvider>)

    expect(screen.getByText('Dashboard')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Deutsch' }))

    expect(screen.getByText('Übersicht')).toBeVisible()
    expect(screen.getByText('de')).toBeVisible()
    expect(localStorage.getItem('multispeed-language')).toBe('de')
    expect(document.documentElement.lang).toBe('de')
  })
})
