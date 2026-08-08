import { Activity } from 'lucide-react'
import type { ProviderId, ResultStatus } from '../../lib/types'
import { providerLabel, statusTone } from '../../lib/utils'
import { Badge } from '../ui/badge'

export function ProviderBadge({ provider }: { provider: ProviderId }) {
  const tone = provider === 'ookla' ? 'info' : provider === 'librespeed' ? 'violet' : 'warning'
  return <Badge tone={tone}>{providerLabel(provider)}</Badge>
}

export function ResultStatusBadge({ status }: { status: ResultStatus }) {
  const running = status === 'running' || status === 'validating' || status === 'queued'
  return <Badge tone={statusTone[status]} className="gap-1.5 capitalize">{running ? <Activity className="h-3 w-3 animate-pulse" /> : null}{status}</Badge>
}
