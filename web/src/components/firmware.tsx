import { ArrowUpCircle, Check, ChevronDown, ChevronUp, Cpu, Download, ExternalLink, Loader2, RefreshCw, X } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { Row, Section } from '@/components/settings/section'
import { Badge } from '@/components/ui/badge'
import { UpgradeDialog } from '@/components/upgrade-dialog'
import { Button } from '@/components/ui/button'
import { useApp } from '@/hooks/app-context'
import { useEdits } from '@/hooks/mutations'
import { firmwareDate, isNever, timeAgo } from '@/lib/format'
import type { DeviceState, FirmwareStatus, UpgradeStatus } from '@/lib/types'

/** What an install is doing, in words. */
function upgradeText(u: UpgradeStatus): string {
  switch (u.phase) {
    case 'downloading':
      return `Downloading ${firmwareDate(u.release)} from SiliconDust…`
    case 'sending':
      return `Sending it to the tuner… ${u.percent}%`
    case 'restarting':
      return "The tuner is installing it and restarting. This takes a couple of minutes: don't unplug it."
    case 'verifying':
      return 'The tuner is back. Checking what it is running…'
    case 'done':
      return `Updated from ${firmwareDate(u.from)} to ${firmwareDate(u.release)}.`
    default:
      return u.error ?? 'The update did not finish.'
  }
}

export const CHANGELOG_URL = 'https://www.silicondust.com/support/downloads/firmware-changelog.html'

/** A small flag on a tuner that has newer firmware, linking to the details. */
export function FirmwareBadge({ status, to = '/tuners' }: { status: FirmwareStatus; /** Where it goes: the tuner's Firmware tab. */ to?: string }) {
  if (status.state !== 'update-available' && status.state !== 'possible-update') return null
  return (
    <Link to={to} className="inline-flex" title="See what's new">
      <Badge className="gap-1 bg-amber-500/15 text-amber-800 dark:text-amber-300">
        <ArrowUpCircle className="size-3" />
        {status.state === 'update-available' ? 'Firmware update' : 'Firmware: check'}
      </Badge>
    </Link>
  )
}

function StatusBadge({ s }: { s: FirmwareStatus }) {
  switch (s.state) {
    case 'up-to-date':
      return <Badge className="bg-emerald-500/15 text-emerald-800 dark:text-emerald-300">Up to date</Badge>
    case 'update-available':
      return <Badge className="bg-amber-500/15 text-amber-800 dark:text-amber-300">Update available · {firmwareDate(s.latest)}</Badge>
    case 'possible-update':
      return <Badge className="bg-amber-500/15 text-amber-800 dark:text-amber-300">Newer release may apply</Badge>
    case 'disabled':
      return <Badge variant="secondary">Check is off</Badge>
    default:
      return <Badge variant="secondary">Unknown</Badge>
  }
}

/** One tuner's firmware: where it stands, when SiliconDust's changelog was last read, and what's new. */
export function FirmwareSection({ device }: { device: DeviceState }) {
  const { state } = useApp()
  const { firmware: s, tuner } = device
  const { firmware: check, dismiss } = useEdits()
  const [showAll, setShowAll] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const checking = state.settings.firmwareCheck
  const shown = showAll ? s.updates : s.updates.slice(0, 2)
  const up = device.upgrade
  const running = up !== undefined && up.phase !== 'done' && up.phase !== 'failed'
  // The server decides for itself; this is only so the button isn't offered when it would be refused.
  const canInstall = s.state === 'update-available' && tuner.online && !tuner.updating && !running

  return (
    <Section bare icon={Cpu} title="Firmware" description="The firmware the tuner is running, and installing a newer one.">
      <Row
        title="Version"
        note={`${tuner.firmware.name ?? tuner.model ?? 'Tuner'} · ${s.current || tuner.firmware.version || 'unknown'}`}
      >
        <span className="text-sm font-medium">{firmwareDate(s.current)}</span>
        <StatusBadge s={s} />
        {canInstall && (
          <Button size="sm" onClick={() => setConfirming(true)}>
            <Download data-icon="inline-start" /> Install
          </Button>
        )}
      </Row>

      {up && (
        <Row
          title={running ? 'Updating' : up.phase === 'done' ? 'Updated' : 'The update failed'}
          note={
            <span role={running ? 'status' : undefined} className={up.phase === 'failed' ? 'text-red-700 dark:text-red-300' : undefined}>
              {upgradeText(up)}
            </span>
          }
        >
          {running && <Loader2 aria-label="Working" className="size-4 animate-spin text-muted-foreground" />}
          {up.phase === 'done' && <Check aria-hidden className="size-4 text-emerald-600 dark:text-emerald-400" />}
          {!running && (
            <Button size="sm" variant="outline" disabled={dismiss.isPending} onClick={() => dismiss.mutate({ device: device.name })}>
              <X data-icon="inline-start" /> Dismiss
            </Button>
          )}
        </Row>
      )}

      <Row
        title="SiliconDust's changelog"
        note={
          <>
            {isNever(s.checkedAt) ? 'Not read yet.' : `Last read ${timeAgo(s.checkedAt)}.`}
            {s.deviceReported && <> The tuner itself reports {firmwareDate(s.deviceReported)} as available.</>}
            {s.error && <> {capitalize(s.error)}.</>}
          </>
        }
      >
        <Button
          size="sm"
          variant="outline"
          disabled={!checking || check.isPending}
          onClick={() => check.mutate()}
          title={checking ? undefined : 'The firmware check is turned off in Settings'}
        >
          {check.isPending ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <RefreshCw data-icon="inline-start" />}
          {check.isPending ? 'Checking…' : 'Check now'}
        </Button>
        <a
          href={CHANGELOG_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
        >
          Changelog <ExternalLink className="size-3" />
        </a>
      </Row>

      {s.updates.length > 0 && (
        <div className="space-y-3 px-4 py-3.5">
          <div className="text-sm font-medium">What's new since {firmwareDate(s.current)}</div>
          <ul className="space-y-3">
            {shown.map((u) => (
              <li key={u.version}>
                <div className="flex items-center gap-2 text-sm font-medium">
                  {firmwareDate(u.version)}
                  {!u.certain && <Badge variant="secondary">may not apply to your model</Badge>}
                </div>
                <ul className="mt-1 list-disc space-y-0.5 pl-5 text-xs text-muted-foreground">
                  {u.notes.map((n, i) => (
                    <li key={i}>
                      {n.scope && <span className="font-medium text-foreground">{n.scope}: </span>}
                      {n.text}
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
          {s.updates.length > 2 && (
            <Button size="sm" variant="ghost" onClick={() => setShowAll(!showAll)}>
              {showAll ? <ChevronUp data-icon="inline-start" /> : <ChevronDown data-icon="inline-start" />}
              {showAll ? 'Show fewer' : `Show all ${s.updates.length} releases`}
            </Button>
          )}
          {!s.modelRecognized && (
            <p className="text-xs text-muted-foreground">Yagi doesn't recognize this model, so it can't tell which notes are for it; they are all shown.</p>
          )}
          <p className="text-xs text-muted-foreground">
            {canInstall
              ? 'Install downloads the newest release from SiliconDust and puts it on the tuner. You can also update from the tuner’s own page or with SiliconDust’s HDHomeRun software.'
              : 'You can update from the tuner’s own page or with SiliconDust’s HDHomeRun software, or install it here when the tuner is online and a newer release applies.'}
          </p>
        </div>
      )}
      <UpgradeDialog device={confirming ? device : null} onClose={() => setConfirming(false)} />
    </Section>
  )
}

const capitalize = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)
