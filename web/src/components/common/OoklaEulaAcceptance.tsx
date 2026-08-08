import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, ShieldCheck, ShieldOff } from 'lucide-react'
import { useState } from 'react'
import { useAppSettings } from '../../hooks/useAppSettings'
import { api, getApiErrorMessage } from '../../lib/api'
import { queryKeys } from '../../lib/query'
import { formatDateTime } from '../../lib/utils'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { ConfirmDialog } from '../ui/dialog'
import { CheckField } from '../ui/fields'
import { Spinner } from '../ui/states'
import { useToast } from '../ui/toast'

const ooklaEulaUrl = 'https://www.speedtest.net/about/eula'

export function OoklaEulaAcceptance({ compact = false }: { compact?: boolean }) {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const { settings } = useAppSettings()
  const [confirmed, setConfirmed] = useState(false)
  const [confirmRevoke, setConfirmRevoke] = useState(false)
  const mutation = useMutation({
    mutationFn: ({ accepted, explicitConfirmation }: { accepted: boolean; explicitConfirmation: boolean }) => api.updateOoklaEula(accepted, explicitConfirmation),
    onSuccess: (updated, request) => {
      queryClient.setQueryData(queryKeys.settings, updated)
      void queryClient.invalidateQueries({ queryKey: queryKeys.providers })
      void queryClient.invalidateQueries({ queryKey: queryKeys.system })
      setConfirmed(false)
      setConfirmRevoke(false)
      toast({
        tone: 'success',
        title: request.accepted ? 'Ookla EULA acceptance recorded' : 'Ookla EULA acceptance revoked',
        description: request.accepted
          ? 'Provider checks now continue to the separately installed Ookla CLI.'
          : updated.ooklaEulaEffectiveAccepted
            ? 'The persisted record was removed; ACCEPT_OOKLA_EULA still keeps the technical gate open.'
            : 'New Ookla discovery and test runs are blocked.',
      })
    },
    onError: (error) => toast({ tone: 'error', title: 'Unable to update Ookla acceptance', description: getApiErrorMessage(error) }),
  })

  if (settings.ooklaEulaEffectiveAccepted) {
    const environmentOverride = settings.ooklaEulaAcceptanceSource === 'environment'
    return (
      <div className={compact ? 'mt-3 space-y-3' : 'space-y-4'}>
        <div className="flex flex-wrap items-center gap-2">
          <Badge tone="success" className="gap-1"><ShieldCheck className="h-3 w-3" />{environmentOverride ? 'Accepted by environment' : 'Acceptance recorded'}</Badge>
          <span className="text-xs text-muted-foreground">
            {environmentOverride
              ? 'ACCEPT_OOKLA_EULA=true'
              : settings.ooklaEulaAcceptedAt ? formatDateTime(settings.ooklaEulaAcceptedAt, true, settings.defaultTimezone) : 'Timestamp unavailable'}
          </span>
        </div>
        <p className="text-xs leading-5 text-muted-foreground">
          {environmentOverride
            ? 'The environment override opens MultiSpeed\'s technical gate. Clear ACCEPT_OOKLA_EULA and restart MultiSpeed to block Ookla runs.'
            : 'This only opens MultiSpeed\'s technical gate. It does not grant a license, install the proprietary CLI, or replace any permission required by Ookla.'}
        </p>
        <p className="text-xs text-muted-foreground">Terms revision: <code>{settings.ooklaEulaCurrentVersion}</code></p>
        <div className="flex flex-wrap gap-2">
          <Button asChild size="sm" variant="outline"><a href={ooklaEulaUrl} target="_blank" rel="noopener noreferrer"><ExternalLink className="h-3.5 w-3.5" />Review current EULA</a></Button>
          {environmentOverride ? null : <Button size="sm" variant="ghost" onClick={() => setConfirmRevoke(true)} disabled={mutation.isPending}><ShieldOff className="h-3.5 w-3.5" />Revoke acceptance</Button>}
        </div>
        {environmentOverride ? null : <ConfirmDialog open={confirmRevoke} onOpenChange={setConfirmRevoke} title="Revoke Ookla EULA acceptance?" description="New Ookla provider discovery and test runs will be blocked. Existing result history is preserved." confirmLabel="Revoke acceptance" destructive busy={mutation.isPending} onConfirm={() => mutation.mutate({ accepted: false, explicitConfirmation: false })} />}
      </div>
    )
  }

  return (
    <div className={compact ? 'mt-3 space-y-3' : 'space-y-4'}>
      {settings.ooklaEulaVersion && settings.ooklaEulaVersion !== settings.ooklaEulaCurrentVersion
        ? <p className="text-xs leading-5 text-amber-500">The previous acknowledgement covered <code>{settings.ooklaEulaVersion}</code>. Review and accept the currently required revision again.</p>
        : null}
      <p className="text-xs leading-5 text-muted-foreground">Review Ookla's current terms and obtain any permission required for this installation. MultiSpeed never downloads or bundles the proprietary CLI.</p>
      <p className="text-xs text-muted-foreground">Required terms revision: <code>{settings.ooklaEulaCurrentVersion}</code></p>
      <Button asChild size="sm" variant="outline"><a href={ooklaEulaUrl} target="_blank" rel="noopener noreferrer"><ExternalLink className="h-3.5 w-3.5" />Review current Ookla EULA</a></Button>
      <CheckField checked={confirmed} onChange={setConfirmed} label="I reviewed and accept the current Ookla EULA" description="I confirm that I am authorized to record this decision for this MultiSpeed installation." />
      <Button size="sm" onClick={() => mutation.mutate({ accepted: true, explicitConfirmation: true })} disabled={!confirmed || mutation.isPending}>{mutation.isPending ? <Spinner /> : <ShieldCheck className="h-3.5 w-3.5" />}Record acceptance</Button>
    </div>
  )
}
