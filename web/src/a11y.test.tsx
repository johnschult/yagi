import { act, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { axe } from 'vitest-axe'
import { App } from '@/App'
import { SEEN_KEY } from '@/hooks/use-whats-new'
import { discord, makeDevice, makeState, makeTwoDeviceState, phone, pushover, updateAvailable, withChannels } from '@/test/fixtures'
import { mockPhone } from '@/test/match-media'
import { renderWithApp, stubFetch } from '@/test/render'

// axe asks a canvas to measure text, which jsdom doesn't have and complains about at length. It can answer "no".
beforeEach(() => {
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

/**
 * jsdom does no layout, so axe can't judge color contrast here, or whether something is hidden by CSS.
 * Those need a real browser. Everything else axe knows (names, roles, labels, structure) is checked.
 * Dialogs render in a portal, so check the whole body.
 */
const check = () => axe(document.body, { rules: { 'color-contrast': { enabled: false } } })

type State = ReturnType<typeof makeState>
const render = (state: State, route: string) => renderWithApp(<App />, state, { route })

/** The states a page can be in, since a page that is fine with a tuner can fail without one. */
const states: [string, () => State][] = [
  ['one tuner, two viewers', () => makeState()],
  ['two tuners', () => makeTwoDeviceState()],
  ['no tuner yet', () => makeState({ devices: [] })],
  ['no viewers', () => makeState({ viewers: [] })],
  ['an offline tuner', () => makeState({ devices: [makeDevice({ tuner: { ...makeDevice().tuner, online: false } })] })],
  ['newer firmware', () => makeState({ devices: [makeDevice({ firmware: updateAvailable })] })],
  ['settings that cannot be saved', () => makeState({ editable: false })],
  ['notification channels', () => withChannels(makeState(), phone, pushover, discord)],
  ['people asking to use a tuner', () => {
    const base = { device: 'antenna', first: '2026-10-22T12:00:00Z', last: '2026-10-22T12:05:00Z', count: 2 }
    return makeState({
      requests: [
        { ...base, id: 'aaaaaaaaaaaa', label: 'pal@example.com', address: '100.64.0.30', login: 'pal@example.com', node: 'den.tail1.ts.net' },
        { ...base, id: 'bbbbbbbbbbbb', label: '10.8.0.44', address: '10.8.0.44' },
      ],
    })
  }],
]

describe('axe: every page, in each state', () => {
  const pages = [
    ['/', 'Live'],
    ['/viewers', 'Viewers'],
    ['/tuners', 'Tuners'],
    ['/settings', 'Settings: Access'],
    ['/settings/notifications', 'Settings: Notifications'],
    ['/settings/privacy', 'Settings: Privacy'],
    ['/settings/logs', 'Settings: Logs'],
  ]
  for (const [route, name] of pages) {
    it.each(states)(`${name}: %s`, async (_, make) => {
      render(make(), route)
      expect(await check()).toHaveNoViolations()
    })
  }

  it('Settings with Sign in with Plex on, and an account allowed', async () => {
    const s = makeState()
    s.settings.plex = { enabled: true, admins: [{ id: 1, name: 'John' }] }
    render(s, '/settings')
    expect(await check()).toHaveNoViolations()
  })
})

describe('axe: notification dialogs', () => {
  const settings = (state: State = makeState()) => {
    render(state, '/settings/notifications')
    return userEvent.setup()
  }

  it('Add channel: each type, and its Send test', async () => {
    const user = settings()
    await user.click(screen.getByRole('button', { name: /Add channel/ }))
    const dialog = await screen.findByRole('dialog')
    expect(await check()).toHaveNoViolations() // ntfy
    await user.click(within(dialog).getByRole('radio', { name: /Pushover/ }))
    expect(await check()).toHaveNoViolations()
    await user.click(within(dialog).getByRole('radio', { name: /Webhook/ }))
    expect(await check()).toHaveNoViolations()
  })

  it.each([
    ['ntfy', phone],
    ['Pushover', pushover],
    ['webhook', discord],
  ])('Edit a saved %s channel', async (_, channel) => {
    const user = settings(withChannels(makeState(), channel))
    await user.click(screen.getByRole('button', { name: `Edit ${channel.name}` }))
    await screen.findByRole('dialog')
    expect(await check()).toHaveNoViolations()
  })

  it('Remove channel', async () => {
    const user = settings(withChannels(makeState(), phone))
    await user.click(screen.getByRole('button', { name: 'Remove Phone' }))
    await screen.findByRole('alertdialog')
    expect(await check()).toHaveNoViolations()
  })
})

describe("axe: what's new", () => {
  it('after an update, with several releases', async () => {
    localStorage.setItem(SEEN_KEY, '2026.10.2')
    render(makeState({ version: '2026.10.5' }), '/viewers')
    await screen.findByRole('dialog', { name: /What's new/ })
    await screen.findAllByRole('heading', { level: 3 })
    expect(await check()).toHaveNoViolations()
  })

  it('from the footer, for the current release', async () => {
    localStorage.setItem(SEEN_KEY, '2026.10.5')
    render(makeState({ version: '2026.10.5' }), '/viewers')
    await userEvent.setup().click(screen.getByRole('button', { name: "What's new" }))
    await screen.findByRole('dialog', { name: /What's new/ })
    await screen.findByRole('region', { name: 'Release notes' })
    expect(await check()).toHaveNoViolations()
  })
})

describe('axe: Live tiles with the guide', () => {
  it('a tile with its poster, program details and progress', async () => {
    const s = makeState()
    s.devices[0].guide = {
      '5.1': { name: 'WFRV', icon: true, now: { title: 'Evening News', subTitle: 'Local', episode: 'S01E02', category: 'News', description: 'The day in review.', start: '2026-10-17T17:00:00Z', stop: '2026-10-17T18:00:00Z', poster: 'ABC123' } },
    }
    render(s, '/')
    await screen.findByTestId('tile-progress')
    expect(await check()).toHaveNoViolations()
  })
})

describe('axe: the Activity page', () => {
  it('with sessions, a chart and day groups', async () => {
    const at = (d: number, h: number, m = 0) => new Date(2026, 9, d, h, m).toISOString()
    const base = { device: 'antenna', tuner: 'tuner2', channel: '2.1', channelName: 'WSB-HD' }
    vi.stubGlobal('fetch', stubFetch({ 'GET /state': () => ({ body: makeState() }), 'GET /history?days=7': () => ({ body: { days: 7, now: at(19, 21), sessions: [
      { ...base, kind: 'viewer', viewer: 'Alex', start: at(19, 20), end: at(19, 21, 30), titles: ['College Football'], open: true },
      { ...base, kind: 'local', client: '192.168.1.5', tuner: 'tuner3', start: at(18, 8), end: at(18, 9) },
    ] } }) }).fn)
    render(makeState(), '/activity')
    await screen.findAllByTestId('session')
    expect(await check()).toHaveNoViolations()
  })

  it('with nothing yet', async () => {
    vi.stubGlobal('fetch', stubFetch({ 'GET /state': () => ({ body: makeState() }), 'GET /history?days=7': () => ({ body: { days: 7, now: new Date().toISOString(), sessions: [] } }) }).fn)
    render(makeState(), '/activity')
    await screen.findByText(/Nothing watched/)
    expect(await check()).toHaveNoViolations()
  })
})

describe('axe: a viewer’s page', () => {
  it('with data, time, channels and sessions', async () => {
    const at = (d: number, h: number, m = 0) => new Date(2026, 9, d, h, m).toISOString()
    vi.stubGlobal('fetch', stubFetch({ 'GET /state': () => ({ body: makeState() }), 'GET /history?days=30': () => ({ body: { days: 30, now: at(19, 21), sessions: [
      { device: 'antenna', tuner: 'tuner2', channel: '2.1', channelName: 'WSB-HD', kind: 'viewer', viewer: 'John', start: at(19, 20), end: at(19, 21, 30), titles: ['College Football'], bytes: 2_000_000_000, quality: 62, open: true },
      { device: 'antenna', tuner: 'tuner3', channel: '5.1', channelName: 'WFRV', kind: 'viewer', viewer: 'John', start: at(18, 8), end: at(18, 9), bytes: 800_000_000, quality: 95 },
    ] } }) }).fn)
    render(makeState(), '/viewers/John')
    await screen.findAllByTestId('session')
    expect(await check()).toHaveNoViolations()
  })
})

describe('axe: the tuner pages', () => {
  it('the list of several tuners', async () => {
    render(makeTwoDeviceState(), '/tuners')
    await screen.findByRole('list', { name: 'Tuners' })
    expect(await check()).toHaveNoViolations()
  })

  it.each(['', '/firmware', '/settings'])('a tuner’s page at %s', async (tab) => {
    vi.stubGlobal('fetch', stubFetch({ 'GET /state': () => ({ body: makeState() }) }).fn)
    render(makeState({ devices: [makeDevice({ firmware: updateAvailable })] }), `/tuners/antenna${tab}`)
    await screen.findByRole('navigation', { name: 'Sections of this tuner' })
    expect(await check()).toHaveNoViolations()
  })
})

describe('axe: channels', () => {
  const channels = [
    { number: '2.1', name: 'WSB-HD', hd: true, video: 'MPEG2', state: 'enabled', subscribed: true },
    { number: '2.2', name: 'Laff', state: 'favorite', subscribed: true },
    { number: '10.3', name: 'Old Station', state: 'disabled', subscribed: true },
    { number: '703', name: 'Locked', state: 'enabled', subscribed: false },
  ]
  const scan = { inProgress: false, possible: true, source: 'Antenna', sources: ['Antenna', 'Cable'] }

  it('the Channels tab, and the scan confirmation', async () => {
    vi.stubGlobal('fetch', stubFetch({ 'GET /state': () => ({ body: makeState() }), 'GET /devices/antenna/channels': () => ({ body: { channels, scan } }) }).fn)
    const s = makeState()
    s.devices[0].summary.inUse = 2
    render(s, '/tuners/antenna/channels')
    await screen.findAllByTestId('channel')
    expect(await check()).toHaveNoViolations()
    await userEvent.setup().click(screen.getByRole('button', { name: 'Scan for channels' }))
    await screen.findByRole('alertdialog')
    expect(await check()).toHaveNoViolations()
  })

  it('the Channels tab while a scan runs', async () => {
    vi.stubGlobal('fetch', stubFetch({ 'GET /state': () => ({ body: makeState() }), 'GET /devices/antenna/channels': () => ({ body: { channels, scan: { ...scan, inProgress: true, possible: false, progress: 35, found: 9 } } }) }).fn)
    render(makeState(), '/tuners/antenna/channels')
    await screen.findByRole('progressbar')
    expect(await check()).toHaveNoViolations()
  })
})

describe('axe: the Device panel', () => {
  const info = { hardwareModel: 'HDFX-4K', model: 'hdhomerun_dvr_atsc3', version: '20260326', drm: 'SD20230420', mac: '00:18:DD:00:00:01', ip: '192.168.1.50', subnet: '255.255.255.0', channelMaps: ['us-bcast'], modulations: ['atsc3'], readAt: new Date().toISOString(), problems: ['the tuner has no System Menu page to read'] }

  it('with the details read, and with a note on what could not be', async () => {
    vi.stubGlobal('fetch', stubFetch({ 'GET /state': () => ({ body: makeState() }), 'GET /devices/antenna/info': () => ({ body: info }) }).fn)
    render(makeState(), '/tuners/antenna/device')
    await screen.findByText('SD20230420')
    expect(await check()).toHaveNoViolations()
  })
})

describe('axe: restarting a tuner', () => {
  it('the confirmation, with something running to interrupt', async () => {
    const s = makeState()
    s.devices[0].summary.inUse = 2
    vi.stubGlobal('fetch', stubFetch({ 'GET /state': () => ({ body: s }), 'GET /devices/antenna/info': () => ({ body: { hardwareModel: 'HDFX-4K', readAt: new Date().toISOString() } }) }).fn)
    render(s, '/tuners/antenna/device')
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Restart' }))
    await screen.findByRole('alertdialog')
    expect(await check()).toHaveNoViolations()
  })
})

describe('axe: installing firmware', () => {
  const upgrading = (u?: { phase: 'downloading' | 'sending' | 'restarting' | 'verifying' | 'done' | 'failed'; error?: string }, over: Partial<ReturnType<typeof makeDevice>> = {}) =>
    makeState({
      devices: [
        makeDevice({
          firmware: { ...updateAvailable, current: '20250815', latest: '20260326' },
          upgrade: u && { release: '20260326', from: '20250815', percent: 60, started: '2026-10-18T10:00:00Z', ...u },
          ...over,
        }),
      ],
    })

  it('the Tuners page offering the update, and its confirmation', async () => {
    render(upgrading(), '/tuners/antenna/firmware')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Install' }))
    await screen.findByRole('alertdialog')
    expect(await check()).toHaveNoViolations()
  })

  it('the confirmation with something running to interrupt', async () => {
    const s = upgrading()
    s.devices[0].summary.inUse = 2
    render(s, '/tuners/antenna/firmware')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Install' }))
    await screen.findByRole('alertdialog')
    expect(await check()).toHaveNoViolations()
  })

  it.each(['downloading', 'sending', 'restarting', 'verifying', 'done', 'failed'] as const)('the page while it is %s', async (phase) => {
    render(upgrading({ phase, error: phase === 'failed' ? 'the tuner has not come back.' : undefined }), '/tuners/antenna/firmware')
    await screen.findByRole('region', { name: 'Firmware' })
    expect(await check()).toHaveNoViolations()
  })

  it('the Live page and the banners while a tuner is updating', async () => {
    const s = upgrading({ phase: 'restarting' }, { tuner: { ...makeDevice().tuner, online: false, updating: true } })
    render(s, '/')
    expect(await check()).toHaveNoViolations()
  })
})

describe('axe: the phone layout', () => {
  it('the pages with the bottom bar', async () => {
    mockPhone(true)
    render(makeState(), '/viewers')
    expect(await check()).toHaveNoViolations()
  })

  it('a dialog open as a sheet', async () => {
    mockPhone(true)
    render(makeState(), '/viewers')
    await userEvent.setup().click(screen.getByRole('button', { name: /Add viewer/ }))
    await screen.findByRole('dialog')
    expect(await check()).toHaveNoViolations()
  })
})

describe('axe: dialogs', () => {
  const open = async (route: string, state: State, button: string | RegExp, role: 'dialog' | 'alertdialog' = 'dialog') => {
    vi.stubGlobal('fetch', stubFetch({ 'GET /network': () => ({ body: { addresses: [{ ip: '100.64.0.9', kind: 'tailscale', interface: 'tailscale0' }], firewall: { tool: 'ufw', active: true } } }) }).fn)
    render(state, route)
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: button }))
    await screen.findByRole(role)
    return user
  }

  it('Add viewer: each of its three steps', async () => {
    vi.stubGlobal('fetch', stubFetch({ 'POST /viewers': () => ({ status: 201, body: {} }), 'GET /state': () => ({ body: makeState() }), 'GET /network': () => ({ body: { addresses: [] } }) }).fn)
    render(makeState(), '/viewers')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /Add viewer/ }))
    const dialog = await screen.findByRole('dialog')
    expect(await check()).toHaveNoViolations()

    await user.type(within(dialog).getByLabelText('Name'), 'Cabin')
    await user.type(within(dialog).getByLabelText('Addresses'), '10.9.0.6')
    await user.click(within(dialog).getByRole('button', { name: 'Next: Connect' }))
    await within(dialog).findByRole('region', { name: 'The address' })
    await user.click(within(dialog).getByRole('switch', { name: /has a firewall/ }))
    expect(await check()).toHaveNoViolations()

    await user.click(within(dialog).getByRole('button', { name: 'Next: Media server' }))
    await within(dialog).findByRole('region', { name: 'In their media server' })
    expect(await check()).toHaveNoViolations()
  })

  it('Add viewer with two tuners (per-device limits)', async () => {
    await open('/viewers', makeTwoDeviceState(), /Add viewer/)
    expect(await check()).toHaveNoViolations()
  })

  it('Connection steps for a viewer', async () => {
    await open('/viewers', makeState(), 'Connection steps for John')
    await act(async () => {})
    expect(await check()).toHaveNoViolations()
  })

  it('Edit viewer', async () => {
    await open('/viewers', makeState(), 'Edit John')
    expect(await check()).toHaveNoViolations()
  })

  it('Allow a request', async () => {
    const s = makeState({
      requests: [{ id: 'aaaaaaaaaaaa', label: 'pal@example.com', address: '100.64.0.30', login: 'pal@example.com', device: 'antenna', first: '2026-10-22T12:00:00Z', last: '2026-10-22T12:05:00Z', count: 1 }],
    })
    await open('/viewers', s, 'Allow pal@example.com')
    expect(await check()).toHaveNoViolations()
  })

  it('Remove viewer', async () => {
    await open('/viewers', makeState(), 'Remove John', 'alertdialog')
    expect(await check()).toHaveNoViolations()
  })

  it('Add tuner', async () => {
    vi.stubGlobal('fetch', stubFetch({ 'POST /discover': () => ({ body: { found: [] } }) }).fn)
    render(makeState(), '/tuners')
    await userEvent.setup().click(screen.getByRole('button', { name: /Add tuner/ }))
    await screen.findByRole('dialog')
    expect(await check()).toHaveNoViolations()
  })

  it('Remove tuner', async () => {
    await open('/tuners/antenna/settings', makeState(), /^Remove/, 'alertdialog')
    expect(await check()).toHaveNoViolations()
  })
})
