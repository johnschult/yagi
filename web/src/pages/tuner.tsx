import { ArrowLeft, ArrowRight, ExternalLink, SlidersHorizontal, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Link, Navigate, useParams } from 'react-router'
import { ChannelLogo } from '@/components/channel-logo'
import { ChannelsPanel } from '@/components/channels-panel'
import { CheckPanel } from '@/components/check-panel'
import { DevicePanel } from '@/components/device-panel'
import { FirmwareSection } from '@/components/firmware'
import { RemoveTunerDialog } from '@/components/remove-tuner-dialog'
import { EditRow } from '@/components/settings/edit-row'
import { Row, Section } from '@/components/settings/section'
import { TabLinks } from '@/components/tab-links'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useApp } from '@/hooks/app-context'
import { useEdits } from '@/hooks/mutations'
import { StatusDot, tunerSubtitle } from '@/pages/tuners'
import { channelIconUrl } from '@/lib/api'
import { deviceTitle } from '@/lib/device'
import { firmwareDate, formatMbps, timeAgo } from '@/lib/format'
import { settingsPath } from '@/lib/settings-tabs'
import { isTunerTab, TUNER_TABS, tunerPath, type TunerTab } from '@/lib/tuner-tabs'
import type { DeviceState, Slot } from '@/lib/types'

const asset = (name: string) => `${import.meta.env.BASE_URL}${name}`

/** The channel on a tuner, with its logo when the guide has one, or "idle". */
function ChannelCell({ s, device }: { s: Slot; device: DeviceState }) {
  if (!s.inUse) return <span className="text-muted-foreground">idle</span>
  return (
    <span className="flex items-center gap-2">
      {s.channel && device.guide[s.channel]?.icon && (
        <ChannelLogo key={s.channel} size="sm" src={channelIconUrl(device.name, s.channel)} name={s.channelName ?? device.guide[s.channel].name ?? 'Channel'} />
      )}
      {`${s.channel ?? '—'} ${s.channelName ?? ''}`}
    </span>
  )
}

const validPort = (text: string) => {
  const n = Number(text)
  return text.trim() === '' || !Number.isInteger(n) || n < 1 || n > 65535 ? 'Enter a port from 1 to 65535.' : null
}

const usedBy = (s: Slot) => (s.viaProxy ? 'Yagi' : (s.targetIP ?? 'Local device'))
const signalOf = (s: Slot) => (s.signalStrength !== undefined ? `${s.signalStrength}% / ${s.signalQuality ?? 0}%` : null)

/** One tuner's slot: idle, or the channel on it, who has it, its signal and its rate. */
function SlotCell({ s, device }: { s: Slot; device: DeviceState }) {
  const signal = signalOf(s)
  return (
    <li className="rounded-lg border border-border bg-background/40 px-3 py-2 text-sm">
      <div className="flex items-center justify-between gap-2">
        <span className="font-mono text-xs text-muted-foreground">{s.resource}</span>
        <ChannelCell s={s} device={device} />
      </div>
      {s.inUse && (
        <div className="mt-1.5 flex flex-wrap items-center justify-between gap-x-3 text-xs text-muted-foreground">
          <span className="break-words">{usedBy(s)}</span>
          <span className="tabular-nums">{[signal, formatMbps(s.bitsPerSecond)].filter(Boolean).join(' · ')}</span>
        </div>
      )}
    </li>
  )
}


/** The tabs, as links, so each has an address that can be opened, shared and gone back to. */
function TunerTabs({ device, current }: { device: DeviceState; current: TunerTab }) {
  return <TabLinks label="Sections of this tuner" tabs={TUNER_TABS} current={current} to={(id) => tunerPath(device.name, id)} />
}

/** A fact about the tuner, as a row: what it is on the left, its value on the right. Where the tuner's page has its details, the value goes there. */
function Fact({ label, value, to }: { label: string; value: string; to?: string }) {
  return (
    <div className="flex items-baseline justify-between gap-6 px-4 py-2.5">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="min-w-0 truncate text-right font-mono text-[13px] font-medium">
        {to ? (
          <Link to={to} className="underline-offset-2 hover:underline" title="Details">
            {value}
          </Link>
        ) : (
          value
        )}
      </dd>
    </div>
  )
}

/** How firmware stands, as one line with a way to its tab. */
function FirmwareLine({ device }: { device: DeviceState }) {
  const s = device.firmware
  const to = tunerPath(device.name, 'firmware')
  const current = s.current || device.tuner.firmware.version
  const text =
    s.state === 'update-available'
      ? `Update available: ${firmwareDate(s.latest)}`
      : s.state === 'possible-update'
        ? 'A newer release may apply'
        : s.state === 'up-to-date'
          ? 'Up to date'
          : s.state === 'disabled'
            ? 'The check is off'
            : 'Not known'
  const amber = s.state === 'update-available' || s.state === 'possible-update'
  return (
    <div data-testid="firmware-line" className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-border bg-card/70 px-4 py-3 text-sm">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="text-xs text-muted-foreground">Firmware</span>
        <span className="font-mono text-[13px] font-medium">{current ? `${firmwareDate(current)}` : 'unknown'}</span>
        <Badge className={amber ? 'bg-amber-500/15 text-amber-800 dark:text-amber-300' : s.state === 'up-to-date' ? 'bg-emerald-500/15 text-emerald-800 dark:text-emerald-300' : undefined} variant={amber || s.state === 'up-to-date' ? undefined : 'secondary'}>
          {text}
        </Badge>
      </div>
      <Link to={to} className="inline-flex items-center gap-1 text-sm text-muted-foreground underline-offset-2 hover:text-foreground hover:underline">
        {amber ? 'See what\'s new' : 'Firmware'} <ArrowRight aria-hidden className="size-3.5" />
      </Link>
    </div>
  )
}

function Overview({ device }: { device: DeviceState }) {
  const { tuner } = device
  const port = device.listen.slice(device.listen.lastIndexOf(':') + 1)
  const toDevice = tunerPath(device.name, 'device')
  const toSettings = tunerPath(device.name, 'settings')
  return (
    <div className="space-y-5">
      {/* How it is right now, first: whether it is up, when it was last heard from, and how many tuners it has. */}
      <div data-testid="status" className="flex flex-wrap items-center gap-x-6 gap-y-2 rounded-xl border border-border bg-card/70 px-4 py-3 text-sm">
        {tuner.updating ? (
          <Link to={tunerPath(device.name, 'firmware')}>
            <Badge className="bg-amber-500/15 text-amber-800 dark:text-amber-300">Updating firmware…</Badge>
          </Link>
        ) : tuner.restarting ? (
          <Link to={toDevice}>
            <Badge className="bg-amber-500/15 text-amber-800 dark:text-amber-300">Restarting…</Badge>
          </Link>
        ) : tuner.online ? (
          <Badge className="bg-emerald-500/15 text-emerald-700 dark:text-emerald-300">Online</Badge>
        ) : (
          <>
            <Badge className="bg-red-500/15 text-red-700 dark:text-red-300">Offline</Badge>
            <Link to={`${settingsPath('logs')}?q=${encodeURIComponent(device.name)}`} className="text-muted-foreground underline underline-offset-2 hover:text-foreground">
              See why in the logs
            </Link>
          </>
        )}
        <span className="text-muted-foreground">
          Last seen <span className="font-medium text-foreground">{timeAgo(tuner.lastSeen)}</span>
        </span>
        <span className="text-muted-foreground">
          <span className="font-medium text-foreground">{tuner.tunerCount}</span> {tuner.tunerCount === 1 ? 'tuner' : 'tuners'}
        </span>
      </div>

      {tuner.slots.length === 0 ? (
        <p className="text-sm text-muted-foreground">No tuner data yet.</p>
      ) : (
        <ul aria-label="Tuners" className="grid gap-2 sm:grid-cols-2">
          {tuner.slots.map((s) => (
            <SlotCell key={s.resource} s={s} device={device} />
          ))}
        </ul>
      )}

      <FirmwareLine device={device} />

      <section aria-label="About this tuner" className="space-y-2">
        <h2 className="px-1 text-sm font-semibold">About this tuner</h2>
        <dl className="divide-y divide-border overflow-hidden rounded-xl border border-border bg-card/70">
          {device.label && <Fact label="Name" value={device.label} to={toSettings} />}
          <Fact label="Device" value={tuner.friendlyName ?? 'Unknown'} to={toDevice} />
          <Fact label="Model" value={tuner.model ?? 'Unknown'} to={toDevice} />
          <Fact label="DeviceID" value={tuner.id || 'Not yet seen'} to={toDevice} />
          <Fact label="Address" value={tuner.baseURL.replace(/^https?:\/\//, '')} to={toDevice} />
          <Fact label="Port" value={port} to={toSettings} />
        </dl>
      </section>
    </div>
  )
}

function Settings({ device, onRemove }: { device: DeviceState; onRemove: () => void }) {
  const { state } = useApp()
  const { device: edit } = useEdits()
  const port = device.listen.slice(device.listen.lastIndexOf(':') + 1)
  return (
    <Section bare icon={SlidersHorizontal} title="Settings" description="How Yagi treats this tuner.">
      <EditRow
        key={`label-${device.name}-${device.label ?? ''}`}
        kind="text"
        maxLength={60}
        id={`setting-${device.name}-label`}
        title="Name"
        description="What it is called on this page. Plex and other media servers always see the device, e.g. HDHomeRun FLEX 4K."
        initial={device.label ?? ''}
        placeholder={device.tuner.friendlyName ?? device.name}
        disabled={!state.editable}
        onSave={(text) => edit.mutateAsync({ name: device.name, patch: { label: text.trim() } })}
      />
      <EditRow
        key={`port-${device.name}-${device.listen}`}
        kind="number"
        min={1}
        max={65535}
        id={`setting-${device.name}-port`}
        title="Port"
        description="Where media servers reach it. Changing it cuts running streams."
        initial={port}
        validate={validPort}
        disabled={!state.editable}
        onSave={async (text) => {
          const host = device.listen.slice(0, device.listen.lastIndexOf(':'))
          await edit.mutateAsync({ name: device.name, patch: { listen: `${host}:${Number(text)}` } })
        }}
      />
      <Row title="Remove this tuner" description="Stops sharing it and cuts its streams. The tuner itself is untouched.">
        <Button variant="destructive" size="sm" disabled={!state.editable} aria-label={`Remove ${deviceTitle(device)}`} onClick={onRemove}>
          <Trash2 data-icon="inline-start" /> Remove
        </Button>
      </Row>
    </Section>
  )
}

/** One tuner: what is on it, what it says about itself, its firmware and its settings, on tabs. */
export function TunerPage() {
  const { name = '', tab } = useParams()
  const { state } = useApp()
  const [removing, setRemoving] = useState<DeviceState | null>(null)
  const device = state.devices.find((d) => d.name === name)
  const back = (
    <Link to="/tuners" className="inline-flex items-center gap-1.5 text-sm text-muted-foreground underline-offset-2 hover:text-foreground hover:underline">
      <ArrowLeft aria-hidden className="size-4" /> Tuners
    </Link>
  )
  if (!device) {
    return (
      <div className="space-y-4">
        {back}
        <h1 className="text-xl font-semibold tracking-tight">No tuner called {name}</h1>
        <p className="text-sm text-muted-foreground">It may have been removed. The tuners there are now are on the Tuners page.</p>
      </div>
    )
  }
  if (tab !== undefined && !isTunerTab(tab)) return <Navigate to={tunerPath(device.name)} replace />
  const current: TunerTab = tab !== undefined && isTunerTab(tab) ? tab : 'overview'
  const busy = device.tuner.slots.filter((s) => s.inUse).length

  return (
    <div className="space-y-4">
      {back}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <StatusDot device={device} />
        <img src={asset('hdhomerun-icon.svg')} alt="" aria-hidden="true" className="size-9 shrink-0 rounded-md" draggable={false} />
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-xl font-semibold tracking-tight">{deviceTitle(device)}</h1>
          <p className="truncate font-mono text-xs text-muted-foreground">{tunerSubtitle(device)}</p>
        </div>
        <span className="text-sm tabular-nums text-muted-foreground">
          {busy} of {device.tuner.tunerCount} in use
        </span>
      </div>

      <TunerTabs device={device} current={current} />

      {/* Rows that are a label and a control read badly stretched across a wide screen, so every tab is as wide as the Settings page. */}
      <div className="max-w-3xl">
        {current === 'overview' && <Overview device={device} />}
        {current === 'channels' && <ChannelsPanel device={device} />}
        {current === 'device' && (
          <div className="space-y-3">
            <DevicePanel device={device} />
            <CheckPanel device={device} />
            <a href={device.tuner.baseURL} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1.5 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline">
              Open the tuner's own page <ExternalLink aria-hidden className="size-3" />
            </a>
          </div>
        )}
        {current === 'firmware' && <FirmwareSection device={device} />}
        {current === 'settings' && <Settings device={device} onRemove={() => setRemoving(device)} />}
      </div>
      <RemoveTunerDialog device={removing} onClose={() => setRemoving(null)} />
    </div>
  )
}
