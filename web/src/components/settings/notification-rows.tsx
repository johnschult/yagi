import { Bell, Loader2, Pencil, Plus, Send, Smartphone, Trash2, Webhook, type LucideIcon } from 'lucide-react'
import { useState } from 'react'
import { ChannelDialog } from '@/components/channel-dialog'
import { RemoveChannelDialog } from '@/components/remove-channel-dialog'
import { Button } from '@/components/ui/button'
import { useApp } from '@/hooks/app-context'
import { useEdits } from '@/hooks/mutations'
import { channelSummary } from '@/lib/notifications'
import type { ChannelType, NotifyChannel, NotifyEvent } from '@/lib/types'
import { Row } from './section'
import { SwitchRow } from './switch-row'

const EVENTS: { id: NotifyEvent; title: string; description: string }[] = [
  { id: 'firmware', title: 'New tuner firmware', description: 'When SiliconDust has a newer release for a tuner. Once per release, not at every check.' },
  { id: 'tuner', title: 'A tuner goes offline, and comes back', description: 'Only after it has been unreachable for two minutes, so a reboot or a brief drop is not a message.' },
  { id: 'viewer', title: 'A viewer is turned away', description: "A viewer asked for a tuner but was at their limit, or the viewers' shared tuners or your reserved ones were taken. The same one is reported at most every half hour." },
  { id: 'watching', title: 'A viewer starts watching', description: 'Off by default. Says who started watching which channel on which tuner. A viewer is reported at most once every half hour per tuner, so changing channels or a media server connecting again is not a message each time.' },
  { id: 'request', title: 'Someone asks to use a tuner', description: 'When a person who is not a viewer tries to connect for the first time, such as a friend who has just added Yagi in their media server. They appear on the Viewers page, where you can allow them.' },
  { id: 'unknown', title: 'An address that is not a viewer connects', description: 'Off by default: a tuner port that can be reached from the internet is scanned all day. Each address is reported once a day, and at most six an hour in all.' },
]

const ICONS: Record<ChannelType, LucideIcon> = { ntfy: Bell, pushover: Smartphone, webhook: Webhook }

function ChannelRow({ channel, editable, onEdit, onRemove }: { channel: NotifyChannel; editable: boolean; onEdit: () => void; onRemove: () => void }) {
  // Each row has its own, so one test's spinner doesn't show on the others.
  const { channelTest } = useEdits()
  const Icon = ICONS[channel.type]
  return (
    <Row title={channel.name} icon={<Icon aria-hidden className="size-5 text-muted-foreground" />} note={channelSummary(channel)}>
      <Button size="sm" variant="outline" disabled={!editable || channelTest.isPending} onClick={() => channelTest.mutate({ name: channel.name, request: { id: channel.id } })} aria-label={`Send a test to ${channel.name}`}>
        {channelTest.isPending ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Send data-icon="inline-start" />}
        {channelTest.isPending ? 'Sending…' : 'Test'}
      </Button>
      <Button size="icon" variant="ghost" disabled={!editable} onClick={onEdit} aria-label={`Edit ${channel.name}`}>
        <Pencil />
      </Button>
      <Button size="icon" variant="ghost" disabled={!editable} onClick={onRemove} aria-label={`Remove ${channel.name}`}>
        <Trash2 />
      </Button>
    </Row>
  )
}

/** The Notifications section's rows: which events send a message, and where the messages go. */
export function NotificationRows() {
  const { state } = useApp()
  const { channels, events } = state.settings.notifications
  const { editable } = state
  const { settings: update } = useEdits()
  const [dialog, setDialog] = useState<{ channel?: NotifyChannel } | null>(null)
  const [removing, setRemoving] = useState<NotifyChannel | null>(null)

  return (
    <>
      {EVENTS.map((e) => {
        // There is nothing to be told about firmware while the check is off, so it can't be switched on until the check is.
        const blocked = e.id === 'firmware' && !state.settings.firmwareCheck
        return (
          <SwitchRow
            key={e.id}
            id={`setting-notify-${e.id}`}
            title={e.title}
            description={e.description}
            note={blocked ? 'Turn on “Check for new tuner firmware” under Settings → Privacy first.' : undefined}
            checked={events[e.id] && !blocked}
            disabled={!editable || blocked}
            onChange={(next) => update.mutateAsync({ notifyEvents: { [e.id]: next } })}
          />
        )
      })}
      {channels.map((c) => (
        <ChannelRow key={c.id} channel={c} editable={editable} onEdit={() => setDialog({ channel: c })} onRemove={() => setRemoving(c)} />
      ))}
      <Row
        title={channels.length === 0 ? 'Where to send them' : 'Add another'}
        note={channels.length === 0 ? 'Nothing is sent, and nothing is contacted, until you add one. Every channel gets every message you switch on.' : undefined}
      >
        <Button size="sm" variant="outline" disabled={!editable || channels.length >= 8} onClick={() => setDialog({})}>
          <Plus data-icon="inline-start" /> Add channel
        </Button>
      </Row>
      <ChannelDialog open={dialog !== null} onOpenChange={(open) => !open && setDialog(null)} channel={dialog?.channel} />
      <RemoveChannelDialog channel={removing} onClose={() => setRemoving(null)} />
    </>
  )
}
