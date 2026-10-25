import { ExternalLink } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { TailnetOnlyDialog } from '@/components/tailnet-only-dialog'
import { buttonVariants } from '@/components/ui/button'
import { useApp } from '@/hooks/app-context'
import { useEdits } from '@/hooks/mutations'
import { STATE_KEY } from '@/hooks/use-live-state'
import type { TailscaleStatus } from '@/lib/types'
import { EditRow } from './edit-row'
import { Row } from './section'
import { SwitchRow } from './switch-row'

const DOCS = 'https://yagi.johnschult.com/tailscale/'

/** The Tailscale part of the Access section: join your tailnet from inside Yagi, with no Tailscale on the machine. */
export function TailscaleRows() {
  const { state } = useApp()
  const { editable } = state
  const { tailscale: set } = state.settings
  const status: TailscaleStatus = state.tailscale ?? { built: false, state: 'unavailable', hostname: set.hostname }
  const { settings: update } = useEdits()
  const qc = useQueryClient()
  const [closing, setClosing] = useState(false)

  // The node's progress isn't pushed to the page, so while it is still getting somewhere, look again.
  const settling = set.enabled && (status.state === 'starting' || status.state === 'needs-login')
  useEffect(() => {
    if (!settling) return
    const t = setInterval(() => void qc.invalidateQueries({ queryKey: STATE_KEY }), 2500)
    return () => clearInterval(t)
  }, [settling, qc])

  if (!status.built) {
    return (
      <Row
        title="Join your Tailscale network"
        note={
          <>
            This build of Yagi doesn&apos;t include Tailscale. The <code>-tailscale</code> image does: change the image tag in your compose file.{' '}
            <a href={DOCS} target="_blank" rel="noreferrer noopener" className="underline underline-offset-2">
              How
            </a>
          </>
        }
      >
        <span className="text-sm text-muted-foreground">Not in this build</span>
      </Row>
    )
  }

  return (
    <>
      <SwitchRow
        id="setting-tailscale"
        title="Join your Tailscale network"
        description="Yagi becomes its own device on your tailnet. It doesn't need Tailscale installed on this machine."
        checked={set.enabled}
        disabled={!editable}
        onChange={(next) => update.mutateAsync({ tailscaleEnabled: next })}
      />
      {set.enabled && (
        <>
          {(status.state === 'running' || set.tailnetOnly) && (
            <SwitchRow
              id="setting-tailnetOnly"
              title="Only on the tailnet"
              description="Refuse everything that comes in over your network's own address. Yagi then answers on the tailnet, and to this machine itself, and nothing else."
              note={!set.tailnetOnly ? undefined : status.lanClosed ? 'On. Your network’s own address is refused: Yagi answers on the tailnet, and to this machine itself.' : 'On, but not in force: the LAN stays open while the tailnet is not connected (or Yagi was started with -allow-lan).'}
              checked={set.tailnetOnly}
              disabled={!editable}
              onChange={async (next) => {
                if (next) setClosing(true) // asks first; the switch moves when it is saved
                else await update.mutateAsync({ tailnetOnly: false })
              }}
            />
          )}
          {(status.state === 'running' || set.https) && (
            <SwitchRow
              id="setting-tailscaleHttps"
              title="HTTPS for the admin page"
              description="Also serve this page over HTTPS at the device’s tailnet name, with a certificate from Tailscale. It needs HTTPS certificates turned on for your tailnet (Tailscale admin console → DNS), and the name then appears in a public certificate log. The tuner ports stay plain HTTP."
              note={httpsNote(set.https, status)}
              checked={set.https}
              disabled={!editable}
              onChange={(next) => update.mutateAsync({ tailscaleHttps: next })}
            />
          )}
          <EditRow
            key={`ts-host-${set.hostname}`}
            kind="text"
            maxLength={63}
            id="setting-tailscaleHostname"
            title="Name on the tailnet"
            description="Lowercase letters, numbers and hyphens. Changing it restarts the connection."
            initial={set.hostname}
            validate={(t) => (/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/i.test(t.trim()) ? null : 'Use letters, numbers and hyphens, not starting or ending with a hyphen.')}
            disabled={!editable}
            onSave={(text) => update.mutateAsync({ tailscaleHostname: text.trim() })}
          />
          <TailscaleStatusRow status={status} />
          <TailnetOnlyDialog open={closing} onClose={() => setClosing(false)} status={status} />
        </>
      )}
    </>
  )
}

function TailscaleStatusRow({ status }: { status: TailscaleStatus }) {
  return (
    <Row title="Status" note={describe(status)}>
      {status.state === 'needs-login' && status.loginUrl && (
        <a href={status.loginUrl} target="_blank" rel="noreferrer noopener" className={buttonVariants({ size: 'sm' })}>
          <ExternalLink data-icon="inline-start" />
          Sign in to Tailscale
        </a>
      )}
      {status.state === 'running' && (
        <div className="w-full min-w-0 text-sm sm:w-72 sm:text-right">
          {status.dnsName && <p className="truncate font-mono">{status.dnsName}</p>}
          {status.addresses?.map((a) => (
            <p key={a} className="truncate font-mono text-xs text-muted-foreground">
              {a}
            </p>
          ))}
        </div>
      )}
    </Row>
  )
}

/** What the HTTPS row says: what it does and costs, then where it is or why it is not. */
function httpsNote(on: boolean, s: TailscaleStatus): ReactNode {
  if (!on) return undefined
  if (s.https) {
    return (
      <>
        On: <a href={s.https} target="_blank" rel="noreferrer noopener" className="font-mono underline underline-offset-2">{s.https}</a>
      </>
    )
  }
  return s.httpsError ? `Not running yet: ${s.httpsError}. Check that HTTPS certificates are turned on for your tailnet (Tailscale admin console → DNS).` : 'Starting…'
}

function describe(s: TailscaleStatus): string {
  switch (s.state) {
    case 'starting':
      return 'Connecting…'
    case 'needs-login':
      return s.loginUrl ? 'Not signed in yet. Sign in with the link, then approve the new device if your tailnet asks.' : 'Waiting for a sign-in link…'
    case 'running':
      return s.serving?.length
        ? `Connected, and answering on ${s.serving.length === 1 ? 'port' : 'ports'} ${s.serving.join(', ')}. Share this device with the people who should reach it, from the Tailscale admin console.`
        : 'Connected. Share this device with the people who should reach it, from the Tailscale admin console.'
    case 'no-store':
      return 'Yagi was started without a settings file, so there is nowhere to keep this device’s identity. Start it with -config.'
    case 'error':
      return `It could not start: ${s.error ?? 'unknown error'}`
    default:
      return 'Off.'
  }
}
