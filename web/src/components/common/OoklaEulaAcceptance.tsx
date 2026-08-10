import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, FileCheck2, ShieldCheck, ShieldOff, Upload } from 'lucide-react'
import { useRef, useState } from 'react'
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
const ooklaCliDownloadUrl = 'https://www.speedtest.net/apps/cli'

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
        <OoklaBinaryUpload />
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

function OoklaBinaryUpload() {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const input = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [confirmed, setConfirmed] = useState(false)
  const statusQuery = useQuery({ queryKey: queryKeys.ooklaBinary, queryFn: api.ooklaBinaryStatus })
  const upload = useMutation({
    mutationFn: (selected: File) => api.uploadOoklaBinary(selected),
    onSuccess: (result) => {
      queryClient.setQueryData(queryKeys.ooklaBinary, result)
      void queryClient.invalidateQueries({ queryKey: queryKeys.providers })
      void queryClient.invalidateQueries({ queryKey: queryKeys.system })
      setFile(null)
      setConfirmed(false)
      if (input.current) input.current.value = ''
      toast({ tone: 'success', title: 'Ookla executable installed', description: `${result.version} is ready for provider validation.` })
    },
    onError: (error) => toast({ tone: 'error', title: 'Ookla executable rejected', description: getApiErrorMessage(error) }),
  })
  const status = statusQuery.data
  const tooLarge = file !== null && status !== undefined && file.size > status.maxUploadBytes

  return (
    <div className="space-y-3 rounded-lg border border-border bg-background/70 p-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="text-xs font-semibold text-foreground">Ookla executable</p>
          <p className="mt-1 text-[11px] leading-5 text-muted-foreground">MultiSpeed accepts one separately obtained Linux amd64 executable and stores it persistently outside the image.</p>
        </div>
        {status?.installed ? <Badge tone="success" className="gap-1"><FileCheck2 className="h-3 w-3" />Installed</Badge> : null}
      </div>
      {statusQuery.isLoading ? <p className="text-xs text-muted-foreground">Checking manual upload support…</p> : null}
      {statusQuery.error ? <p className="text-xs text-rose-600 dark:text-rose-400">{getApiErrorMessage(statusQuery.error)}</p> : null}
      {status && !status.uploadEnabled ? <p className="text-xs leading-5 text-amber-700 dark:text-amber-300">{status.message} Set <code>APP_ALLOW_OOKLA_BINARY_UPLOAD=true</code> and use the managed binary path to enable it.</p> : null}
      {status?.uploadEnabled ? <>
        <input ref={input} className="sr-only" type="file" aria-label="Ookla Speedtest executable" onChange={(event) => { setFile(event.target.files?.[0] ?? null); setConfirmed(false) }} />
        <div className="flex flex-wrap items-center gap-2">
          <Button type="button" size="sm" variant="outline" onClick={() => input.current?.click()} disabled={upload.isPending}><Upload className="h-3.5 w-3.5" />Choose executable</Button>
          <Button asChild size="sm" variant="outline"><a href={ooklaCliDownloadUrl} target="_blank" rel="noopener noreferrer"><ExternalLink className="h-3.5 w-3.5" />Get official Speedtest CLI</a></Button>
          <span className="min-w-0 truncate text-xs text-muted-foreground">{file ? `${file.name} · ${(file.size / (1024 * 1024)).toFixed(1)} MiB` : `Maximum ${(status.maxUploadBytes / (1024 * 1024)).toFixed(0)} MiB`}</span>
        </div>
        <p className="text-[11px] leading-5 text-muted-foreground">On Ookla's download page, choose the Linux x86_64 archive, extract it, and select the contained <code>speedtest</code> executable here.</p>
        {tooLarge ? <p role="alert" className="text-xs text-rose-600 dark:text-rose-400">The selected file exceeds the {Math.round(status.maxUploadBytes / (1024 * 1024))} MiB upload limit.</p> : null}
        <CheckField checked={confirmed} onChange={setConfirmed} disabled={!file || tooLarge || upload.isPending} label="This is an authorized Speedtest by Ookla executable" description="I obtained this Linux amd64 file separately, reviewed Ookla's terms, and understand that MultiSpeed will execute it inside the container." />
        <Button type="button" size="sm" onClick={() => file && upload.mutate(file)} disabled={!file || !confirmed || tooLarge || upload.isPending}>{upload.isPending ? <Spinner /> : <Upload className="h-3.5 w-3.5" />}Install executable</Button>
        <p className="text-[11px] leading-5 text-muted-foreground">Because MultiSpeed has no authentication, keep this opt-in upload endpoint limited to a trusted private network.</p>
      </> : null}
    </div>
  )
}
