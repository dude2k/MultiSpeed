import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

export type Language = 'en' | 'de'

const storageKey = 'multispeed-language'

const german: Record<string, string> = {
  'Dashboard': 'Übersicht',
  'Tasks': 'Aufgaben',
  'Results': 'Ergebnisse',
  'Statistics': 'Statistiken',
  'WAN comparison': 'WAN-Vergleich',
  'Network & routes': 'Netzwerk & Routen',
  'Settings': 'Einstellungen',
  'System information': 'Systeminformationen',
  'Live overview': 'Live-Überblick',
  'Schedules & targets': 'Zeitpläne & Ziele',
  'Test history': 'Messverlauf',
  'Trends & percentiles': 'Trends & Perzentile',
  'Path performance': 'Pfad-Leistung',
  'Bindings & validation': 'Bindungen & Prüfung',
  'Runtime defaults': 'Laufzeit-Standardwerte',
  'Health & versions': 'Zustand & Versionen',
  'Operations overview': 'Betriebsübersicht',
  'Multi-WAN observability': 'Multi-WAN-Überwachung',
  'Speed-test tasks': 'Geschwindigkeitstest-Aufgaben',
  'Independent schedules': 'Unabhängige Zeitpläne',
  'Create task': 'Aufgabe erstellen',
  'Measurement configuration': 'Messkonfiguration',
  'Measurement results': 'Messergebnisse',
  'History & diagnostics': 'Verlauf & Diagnose',
  'Performance statistics': 'Leistungsstatistiken',
  'Aggregates & trends': 'Aggregate & Trends',
  'Side-by-side analysis': 'Direktvergleich',
  'Read-only path validation': 'Schreibgeschützte Pfadprüfung',
  'Application settings': 'Anwendungseinstellungen',
  'Defaults & retention': 'Standardwerte & Aufbewahrung',
  'Runtime health': 'Laufzeitzustand',
  'Edit task': 'Aufgabe bearbeiten',
  'Result diagnostics': 'Ergebnisdiagnose',
  'Infrastructure observability': 'Infrastrukturüberwachung',
  'Path telemetry': 'Pfad-Telemetrie',
  'Light theme': 'Helles Design',
  'Dark theme': 'Dunkles Design',
  'System theme': 'Systemdesign',
  'Color theme': 'Farbschema',
  'Language': 'Sprache',
  'English': 'Englisch',
  'German': 'Deutsch',
  'Open navigation': 'Navigation öffnen',
  'Close navigation': 'Navigation schließen',
  'Navigation': 'Navigation',
  'Navigate between MultiSpeed screens': 'Zwischen MultiSpeed-Ansichten wechseln',
  'Live': 'Live',
  'Offline': 'Offline',
  'Reconnecting': 'Verbindung wird wiederhergestellt',
  'Server-sent events connection': 'Server-Sent-Events-Verbindung',
  'No authentication. Keep this console on a trusted network.': 'Keine Anmeldung. Diese Konsole nur in einem vertrauenswürdigen Netzwerk verwenden.',
  'Cloudflare® is a trademark of Cloudflare, Inc. Ookla® and Speedtest® are registered trademarks of Ookla, LLC. MultiSpeed is an independent project and is not affiliated with, endorsed by, or sponsored by either company.': 'Cloudflare® ist eine Marke von Cloudflare, Inc. Ookla® und Speedtest® sind eingetragene Marken von Ookla, LLC. MultiSpeed ist ein unabhängiges Projekt und steht in keiner Verbindung zu diesen Unternehmen.',
  'Loading data…': 'Daten werden geladen…',
  'Unable to load data': 'Daten konnten nicht geladen werden',
  'Try again': 'Erneut versuchen',
  'Dismiss notification': 'Benachrichtigung schließen',
  'Cancel': 'Abbrechen',
  'Working…': 'Wird verarbeitet…',
  'Close': 'Schließen',
  'Opening workspace…': 'Arbeitsbereich wird geöffnet…',
  'Every WAN, one operational picture.': 'Jedes WAN in einer Betriebsübersicht.',
  'Live throughput, route integrity, and scheduled measurements across every configured network path.': 'Live-Durchsatz, Routenintegrität und geplante Messungen für jeden konfigurierten Netzwerkpfad.',
  'New task': 'Neue Aufgabe',
  'Independent tests, precisely routed.': 'Unabhängige Tests, präzise geroutet.',
  'Each task owns its provider, target, schedule, source address, and route-validation policy.': 'Jede Aufgabe besitzt Provider, Ziel, Zeitplan, Quelladresse und Routenprüfrichtlinie.',
  'Every sample, fully explainable.': 'Jede Messung vollständig nachvollziehbar.',
  'Filter measurement history, inspect route and provider diagnostics, or export exactly the selected result set.': 'Messverlauf filtern, Routen- und Providerdiagnosen prüfen oder exakt die ausgewählte Ergebnismenge exportieren.',
  'Patterns beyond a single test.': 'Muster über einzelne Tests hinaus.',
  'Aggregate in the reporting timezone, compare dimensions, and inspect min, average, median, p95, and variance without failed sentinel values.': 'In der Berichtszeitzone aggregieren, Dimensionen vergleichen und Minimum, Durchschnitt, Median, p95 und Varianz ohne fehlgeschlagene Platzhalterwerte prüfen.',
  'Compare paths, not anecdotes.': 'Pfade vergleichen, nicht Anekdoten.',
  'Use exact full-range statistics across successful, failed, skipped, and cancelled attempts to compare independent WAN paths without page truncation.': 'Exakte Statistiken über den gesamten Zeitraum für erfolgreiche, fehlgeschlagene, übersprungene und abgebrochene Versuche nutzen, ohne Seitentrunkierung.',
  'Observe routes. Never rewrite them.': 'Routen beobachten. Niemals verändern.',
  'Discover source addresses in the active Linux namespace and persist read-only expectations for source-based policy routing.': 'Quelladressen im aktiven Linux-Namespace erkennen und schreibgeschützte Erwartungen für quellenbasiertes Policy-Routing speichern.',
  'Refresh interfaces': 'Schnittstellen aktualisieren',
  'Safe operational defaults.': 'Sichere Betriebsstandardwerte.',
  'Tune new-task defaults, concurrency, retention, interface discovery, and database maintenance.': 'Standardwerte für neue Aufgaben, Parallelität, Aufbewahrung, Schnittstellenerkennung und Datenbankwartung festlegen.',
  'Save settings': 'Einstellungen speichern',
  'Runtime facts, without secrets.': 'Laufzeitdaten ohne Geheimnisse.',
  'Build identity, database health, provider availability, interface state, and application uptime.': 'Build-Identität, Datenbankzustand, Provider-Verfügbarkeit, Schnittstellenstatus und Betriebszeit.',
  'Refresh': 'Aktualisieren',
  'Measurement diagnostics': 'Messdiagnose',
  'Delete': 'Löschen',
  'Create an independent speed test': 'Unabhängigen Geschwindigkeitstest erstellen',
  'The selected interface and source address are mandatory. MultiSpeed never falls back to another WAN when binding or route validation fails.': 'Die ausgewählte Schnittstelle und Quelladresse sind erforderlich. MultiSpeed weicht bei Bindungs- oder Routenprüfungsfehlern niemals auf ein anderes WAN aus.',
  'Back to tasks': 'Zurück zu Aufgaben',
  'This task\'s network path is no longer valid': 'Der Netzwerkpfad dieser Aufgabe ist nicht mehr gültig',
  'Select a current interface and concrete source address before saving or running this task.': 'Vor dem Speichern oder Ausführen eine aktuelle Schnittstelle und konkrete Quelladresse auswählen.',
}

function interpolate(value: string, parameters: Record<string, string | number> | undefined): string {
  if (!parameters) return value
  return value.replace(/\{\{(\w+)\}\}/g, (match, key: string) => String(parameters[key] ?? match))
}

interface I18nApi {
  language: Language
  setLanguage: (language: Language) => void
  t: (english: string, parameters?: Record<string, string | number>) => string
}

const I18nContext = createContext<I18nApi | null>(null)

export function I18nProvider({ children }: { children: ReactNode }) {
  const [language, setLanguage] = useState<Language>(() => localStorage.getItem(storageKey) === 'de' ? 'de' : 'en')
  useEffect(() => {
    localStorage.setItem(storageKey, language)
    document.documentElement.lang = language
  }, [language])
  const value = useMemo<I18nApi>(() => ({
    language,
    setLanguage,
    t: (english, parameters) => interpolate(language === 'de' ? (german[english] ?? english) : english, parameters),
  }), [language])
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n(): I18nApi {
  const context = useContext(I18nContext)
  if (!context) throw new Error('useI18n must be used within I18nProvider')
  return context
}
