import { lazy, Suspense } from 'react'
import { Navigate, Route, Routes } from 'react-router'
import { AppShell } from './components/layout/AppShell'
import { LoadingState } from './components/ui/states'

const DashboardPage = lazy(() => import('./pages/DashboardPage'))
const TasksPage = lazy(() => import('./pages/TasksPage'))
const TaskEditorPage = lazy(() => import('./pages/TaskEditorPage'))
const ResultsPage = lazy(() => import('./pages/ResultsPage'))
const ResultDetailPage = lazy(() => import('./pages/ResultDetailPage'))
const StatisticsPage = lazy(() => import('./pages/StatisticsPage'))
const ComparisonPage = lazy(() => import('./pages/ComparisonPage'))
const NetworkPage = lazy(() => import('./pages/NetworkPage'))
const SettingsPage = lazy(() => import('./pages/SettingsPage'))
const SystemPage = lazy(() => import('./pages/SystemPage'))
const NotFoundPage = lazy(() => import('./pages/NotFoundPage'))

export function App() {
  return (
    <AppShell>
      <Suspense fallback={<LoadingState label="Opening workspace…" />}>
        <Routes>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/tasks" element={<TasksPage />} />
          <Route path="/tasks/new" element={<TaskEditorPage />} />
          <Route path="/tasks/:taskId/edit" element={<TaskEditorPage />} />
          <Route path="/results" element={<ResultsPage />} />
          <Route path="/results/:resultId" element={<ResultDetailPage />} />
          <Route path="/statistics" element={<StatisticsPage />} />
          <Route path="/comparison" element={<ComparisonPage />} />
          <Route path="/network" element={<NetworkPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/system" element={<SystemPage />} />
          <Route path="/dashboard" element={<Navigate to="/" replace />} />
          <Route path="*" element={<NotFoundPage />} />
        </Routes>
      </Suspense>
    </AppShell>
  )
}
