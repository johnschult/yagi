import { Bell, Gauge, ShieldCheck, Trash2, Users } from 'lucide-react'
import { useState } from 'react'
import { Link, Navigate, useParams } from 'react-router'
import { ClearHistoryDialog } from '@/components/clear-history-dialog'
import { TabLinks } from '@/components/tab-links'
import { Button } from '@/components/ui/button'

import { EditRow } from '@/components/settings/edit-row'
import { NotificationRows } from '@/components/settings/notification-rows'
import { PlexRows } from '@/components/settings/plex-rows'
import { TailscaleIcon } from '@/components/tailscale-icon'
import { TailscaleRows } from '@/components/settings/tailscale-rows'
import { Row, Section } from '@/components/settings/section'
import { SwitchRow } from '@/components/settings/switch-row'
import { useApp } from '@/hooks/app-context'
import { useEdits } from '@/hooks/mutations'
import { deviceTitle } from '@/lib/device'
import { parseAddresses } from '@/lib/format'
import { isSettingsTab, SETTINGS_TABS, settingsPath, type SettingsTab } from '@/lib/settings-tabs'
import type { DevicePatch } from '@/lib/types'
import { LogsPanel } from '@/pages/logs'

const wholeNumber = (text: string) =>
  text.trim() === '' || !Number.isInteger(Number(text)) || Number(text) < 0 ? 'Enter a whole number, 0 or more.' : null

const TailscaleSectionIcon = ({ className }: { className?: string }) => <TailscaleIcon className={className} />

export function SettingsPage() {
  const { state } = useApp()
  const { settings, devices, editable } = state
  const { device, settings: update } = useEdits()
  const several = devices.length > 1
  const [clearing, setClearing] = useState(false)

  const saveDevice = (name: string, patch: DevicePatch) => device.mutateAsync({ name, patch })

  const params = useParams()
  if (params.tab !== undefined && !isSettingsTab(params.tab)) return <Navigate to={settingsPath()} replace />
  const tab: SettingsTab = isSettingsTab(params.tab) ? params.tab : 'access'

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold tracking-tight">Settings</h1>
      <TabLinks label="Sections of the settings" tabs={SETTINGS_TABS} current={tab} to={settingsPath} />

      {tab === 'logs' ? (
        <LogsPanel />
      ) : (
        <div className="max-w-3xl space-y-8 pt-2">
          {tab === 'access' && (
            <>
          {devices.map((d) => (
            <Section
              key={d.name}
              icon={Gauge}
              title={several ? `Limits: ${deviceTitle(d)}` : 'Limits'}
              description={`How much of ${several ? 'this tuner' : (d.label ?? 'your tuner')} viewers may use.`}
              note={`${d.tuner.friendlyName ?? 'HDHomeRun'} · ${d.tuner.tunerCount} tuners`}
            >
              <EditRow
                key={`reserve-${d.name}-${d.ownerReserve}`}
                kind="number"
                min={0}
                max={d.tuner.tunerCount}
                id={`setting-${d.name}-ownerReserve`}
                title="Tuners reserved for you"
                description="Tuners always held back for your own TVs and apps. Viewers are refused instead of taking the last ones."
                initial={String(d.ownerReserve)}
                validate={wholeNumber}
                disabled={!editable}
                onSave={(text) => saveDevice(d.name, { ownerReserve: Number(text) })}
              />
              <EditRow
                key={`cap-${d.name}-${d.sharedCap}`}
                kind="number"
                min={0}
                max={d.tuner.tunerCount}
                id={`setting-${d.name}-sharedCap`}
                title="Max tuners for viewers"
                description="The most tuners all viewers together may use. 0 means no cap beyond what you reserve."
                initial={String(d.sharedCap)}
                validate={wholeNumber}
                disabled={!editable}
                onSave={(text) => saveDevice(d.name, { sharedCap: Number(text) })}
              />
            </Section>
          ))}


          <Section title="Viewers" icon={Users}>
            <Row title="Who can use your tuners" description="Only the addresses of the viewers you add. With no viewers, nobody can.">
              <Link to="/viewers" className="text-sm font-medium underline underline-offset-2 hover:text-foreground">
                {settings.mode === 'no-viewers' ? 'No viewers yet' : 'Viewers only'}
              </Link>
            </Row>
          </Section>

          <Section title="Admin access" icon={ShieldCheck} description="Who can open this page and change settings.">
            <EditRow
              key={`admin-${settings.admin.join(',')}`}
              kind="lines"
              id="setting-admin"
              title="Admin addresses"
              description={"Addresses allowed to open this page, besides the machine Yagi runs on. One per line: an IP or a range like 192.168.1.0/24. Keep viewers out of these ranges: anyone listed here can change settings. On Yagi’s own Tailscale, a tailnet login works too: tailscale:you@example.com. Take care not to lock yourself out."}
              note={state.tailscale?.lanClosed ? 'While Only on the tailnet is on, addresses on your LAN can’t connect at all: only the machine itself and tailnet addresses matter here.' : undefined}
              initial={settings.admin.join('\n')}
              placeholder="100.64.0.1"
              disabled={!editable}
              onSave={(text) => update.mutateAsync({ admin: parseAddresses(text) })}
            />
            <PlexRows />
          </Section>
          <Section title="Tailscale" icon={TailscaleSectionIcon} description="Yagi can join your tailnet by itself, with no Tailscale on this machine, so the people you share it with reach the admin page and tuners by name, without opening ports.">
            <TailscaleRows />
          </Section>
            </>
          )}
          {tab === 'notifications' && (
            <>
          <Section bare title="Notifications" icon={Bell} description="Messages about firmware, tuners and viewers, to your phone or another service.">
            <NotificationRows />
          </Section>
            </>
          )}
          {tab === 'privacy' && (
            <>
          <Section bare title="Privacy and updates" icon={ShieldCheck} description="What Yagi reads from outside and what it keeps. Nothing about your viewers is ever sent.">
            <SwitchRow
              id="setting-firmwareCheck"
              title="Check for new tuner firmware"
              description="Reads SiliconDust's public firmware changelog once every 12 hours. Nothing about you or your tuners is sent. Yagi tells you about new releases, and installs one only when you press Install on a tuner's Firmware tab."
              checked={settings.firmwareCheck}
              disabled={!editable}
              onChange={(next) => update.mutateAsync({ firmwareCheck: next })}
            />
            <SwitchRow
              id="setting-guide"
              title="Show what's on"
              description="Shows each watched channel's logo and current program on its tile. Yagi reads SiliconDust's program guide for your tuner every 6 hours, which sends SiliconDust your tuner's guide key. Nothing about your viewers is sent, and logos are served by Yagi, not loaded by your browser."
              checked={settings.guide}
              disabled={!editable}
              onChange={(next) => update.mutateAsync({ guide: next })}
            />
            <SwitchRow
              id="setting-history"
              title="Keep activity history"
              description="Remembers who watched what for 30 days, for the Activity page: the viewer's name (or Local for your own devices), the tuner, the channel, the show titles and when it began and ended. A viewer's address is not kept. It stays on this machine, beside the settings file, and nothing is sent anywhere. Turning it off stops recording and hides the page; what was kept stays until you clear it."
              checked={settings.history}
              disabled={!editable}
              onChange={(next) => update.mutateAsync({ history: next })}
            />
            <Row title="Clear activity history" description="Forgets everything that has been kept, however far back. It can't be brought back.">
              <Button variant="outline" size="sm" onClick={() => setClearing(true)}>
                <Trash2 data-icon="inline-start" /> Clear
              </Button>
            </Row>
            <ClearHistoryDialog open={clearing} onClose={() => setClearing(false)} />
          </Section>
            </>
          )}
        </div>
      )}
    </div>
  )
}
