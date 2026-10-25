import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Loader2, RefreshCw, RotateCw, Router, X } from 'lucide-react'
import { useState } from 'react'
import { RestartDialog } from '@/components/restart-dialog'
import { Row, Section } from '@/components/settings/section'
import { Button } from '@/components/ui/button'
import { useEdits } from '@/hooks/mutations'
import { getDeviceInfo } from '@/lib/api'
import { firmwareDate, timeAgo } from '@/lib/format'
import type { DeviceState } from '@/lib/types'

/** What the tuner says about itself. Read when the panel opens and when asked again, not on a timer. */
export function DevicePanel({ device }: { device: DeviceState }) {
  const qc = useQueryClient()
  const key = ['device-info', device.name] as const
  const online = device.tuner.online
  const query = useQuery({
    queryKey: key,
    queryFn: () => getDeviceInfo(device.name),
    enabled: online,
    staleTime: 30_000,
    retry: false,
    refetchOnWindowFocus: false,
  })
  const { dismissRestartResult } = useEdits()
  const [confirming, setConfirming] = useState(false)
  const rs = device.restart
  const restarting = rs?.phase === 'restarting' || device.tuner.restarting
  const [refreshing, setRefreshing] = useState(false)
  const refresh = async () => {
    setRefreshing(true)
    try {
      qc.setQueryData(key, await getDeviceInfo(device.name, true))
    } catch {
      await qc.invalidateQueries({ queryKey: key })
    } finally {
      setRefreshing(false)
    }
  }

  const info = query.data
  const fields: [string, string | undefined][] = info
    ? [
        ['Hardware model', info.hardwareModel],
        ['Firmware', info.version ? `${firmwareDate(info.version)} · ${info.version}` : undefined],
        ['Firmware family', info.model],
        ['ATSC 3.0 DRM', info.drm],
        ['Device ID', info.deviceId ?? device.tuner.id],
        ['MAC address', info.mac],
        ['IP address', info.ip],
        ['Subnet mask', info.subnet],
        ['Channel maps', info.channelMaps?.join(', ')],
        ['Modulation', info.modulations?.join(', ')],
      ]
    : []

  return (
    <Section bare icon={Router} title="Device" description="What the tuner reports about itself, and restarting it.">
      <div className="px-4 py-3.5">
        {!online ? (
          <p className="text-sm text-muted-foreground">The tuner is offline, so there is nothing to read.</p>
        ) : query.isPending ? (
          <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 aria-hidden className="size-4 animate-spin" /> Asking the tuner…
          </p>
        ) : query.isError ? (
          <p role="alert" className="text-sm text-red-700 dark:text-red-300">
            {query.error instanceof Error ? query.error.message : "Couldn't read the tuner."}
          </p>
        ) : (
          <dl className="grid gap-x-8 gap-y-2.5 text-sm sm:grid-cols-2">
            {fields
              .filter(([, v]) => v)
              .map(([label, value]) => (
                <div key={label} className="min-w-0">
                  <dt className="text-xs text-muted-foreground">{label}</dt>
                  <dd className="break-words font-mono text-[13px]">{value}</dd>
                </div>
              ))}
          </dl>
        )}
        {info?.problems?.map((p) => (
          <p key={p} className="mt-3 text-xs text-muted-foreground">
            Not shown: {p}.
          </p>
        ))}
      </div>
      {online && (
        <div className="flex items-center justify-between gap-3 px-4 py-2.5 text-xs text-muted-foreground">
          <span>{info ? `Updated ${timeAgo(info.readAt)}. Read-only: nothing here changes the tuner.` : 'Read-only: nothing here changes the tuner.'}</span>
          <Button size="sm" variant="outline" disabled={refreshing || query.isPending} onClick={refresh}>
            {refreshing ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <RefreshCw data-icon="inline-start" />}
            {refreshing ? 'Refreshing…' : 'Refresh'}
          </Button>
        </div>
      )}

      {rs && (
        <Row
          title={restarting ? 'Restarting' : rs.phase === 'done' ? 'Restarted' : 'The restart failed'}
          note={
            <span role={restarting ? 'status' : undefined} className={rs.phase === 'failed' ? 'text-red-700 dark:text-red-300' : undefined}>
              {restarting ? 'The tuner is restarting. It will be back on its own.' : rs.phase === 'done' ? 'The tuner is back.' : (rs.error ?? 'The restart did not finish.')}
            </span>
          }
        >
          {restarting && <Loader2 aria-label="Working" className="size-4 animate-spin text-muted-foreground" />}
          {rs.phase === 'done' && <Check aria-hidden className="size-4 text-emerald-600 dark:text-emerald-400" />}
          {!restarting && (
            <Button size="sm" variant="outline" disabled={dismissRestartResult.isPending} onClick={() => dismissRestartResult.mutate({ device: device.name })}>
              <X data-icon="inline-start" /> Dismiss
            </Button>
          )}
        </Row>
      )}

      <Row title="Restart" description="Turns the tuner off and on again. It is out of reach for a short while, and whatever is streaming from it is cut off.">
        <Button size="sm" variant="outline" disabled={!online || restarting || device.tuner.updating} onClick={() => setConfirming(true)}>
          <RotateCw data-icon="inline-start" /> Restart
        </Button>
      </Row>
      <RestartDialog device={confirming ? device : null} onClose={() => setConfirming(false)} />
    </Section>
  )
}
