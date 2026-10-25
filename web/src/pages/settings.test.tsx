import type { TailscaleStatus } from '@/lib/types'
import { reachedAnotherWay } from '@/components/tailnet-only-dialog'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fakeWindow, stubOpen } from '@/test/fake-window'
import { makeDevice, makeState, makeTwoDeviceState, noNotifications } from '@/test/fixtures'
import { Route, Routes } from 'react-router'
import { explain, renderSettings, renderWithApp, stubFetch } from '@/test/render'
import { SettingsPage } from './settings'

afterEach(() => vi.unstubAllGlobals())

const state = () => {
  const s = makeState()
  s.settings = { mode: 'enforced', sharedCap: 0, ownerReserve: 1, admin: ['100.64.0.0/10'], firmwareCheck: true, guide: true, history: true, logLevel: 'info', plex: { enabled: false, admins: [] }, tailscale: { enabled: false, hostname: 'yagi', tailnetOnly: false, https: false }, notifications: noNotifications() }
  s.devices[0].ownerReserve = 1
  s.devices[0].sharedCap = 2
  return s
}
const section = (name: string) => screen.getByRole('region', { name })
const saveButton = (scope: HTMLElement) => within(scope).queryByRole('button', { name: 'Save' })
const okState = { 'GET /state': () => ({ body: makeState() }) }

describe('SettingsPage', () => {
  it('groups settings into sections of rows, not a card each', () => {
    renderSettings(state(), 'access')
    for (const name of ['Limits', 'Viewers', 'Admin access', 'Tailscale']) expect(section(name)).toBeInTheDocument()
    expect(screen.queryByText(/^Save$/)).not.toBeInTheDocument() // nothing to save until something changes
    expect(document.querySelector('[data-slot="card"]')).toBeNull()
  })

  it('has four tabs, as links with an address each, and marks the one you are on', () => {
    renderSettings(state(), 'privacy')
    const nav = within(screen.getByRole('navigation', { name: 'Sections of the settings' }))
    expect(nav.getAllByRole('link').map((l) => [l.textContent, l.getAttribute('href')])).toEqual([
      ['Access', '/settings'],
      ['Notifications', '/settings/notifications'],
      ['Privacy', '/settings/privacy'],
      ['Logs', '/settings/logs'],
    ])
    expect(nav.getByRole('link', { name: 'Privacy' })).toHaveAttribute('aria-current', 'page')
    expect(nav.getByRole('link', { name: 'Access' })).not.toHaveAttribute('aria-current')
    expect(nav.getAllByRole('link').every((l) => l.querySelector('svg'))).toBe(true) // an icon on each
  })

  it('shows only the section of the tab you are on', () => {
    const { unmount } = renderSettings(state(), 'privacy')
    expect(screen.getByRole('switch', { name: 'Check for new tuner firmware' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Admin addresses')).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Notifications' })).not.toBeInTheDocument()
    unmount()
    renderSettings(state(), 'notifications')
    expect(screen.getByRole('region', { name: 'Notifications' })).toBeInTheDocument()
    expect(screen.queryByRole('switch', { name: 'Check for new tuner firmware' })).not.toBeInTheDocument()
  })

  it('sends an unknown tab to the first', () => {
    renderWithApp(
      <Routes>
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="/settings/:tab" element={<SettingsPage />} />
      </Routes>,
      state(),
      { route: '/settings/nonsense' },
    )
    expect(screen.getByRole('link', { name: 'Access' })).toHaveAttribute('aria-current', 'page')
  })

  it("shows each setting's current value", () => {
    renderSettings(state(), 'access')
    const tuner = section('Limits')
    expect(within(tuner).getByLabelText('Tuners reserved for you')).toHaveValue(1)
    expect(within(tuner).getByLabelText('Max tuners for viewers')).toHaveValue(2)
    expect(within(tuner).getByText(/4 tuners/)).toBeInTheDocument()
    expect(screen.getByLabelText('Admin addresses')).toHaveValue('100.64.0.0/10')
    expect(within(section('Viewers')).getByRole('link', { name: 'Viewers only' })).toHaveAttribute('href', '/viewers')
  })

  it('says when there are no viewers yet', () => {
    const s = state()
    s.settings.mode = 'no-viewers'
    renderSettings(s, 'access')
    expect(within(section('Viewers')).getByText('No viewers yet')).toBeInTheDocument()
  })

  describe('numbers', () => {
    it('offers Save and Reset only once a value has changed, and Reset puts it back', async () => {
      const user = userEvent.setup()
      renderSettings(state(), 'access')
      const box = screen.getByLabelText('Tuners reserved for you')
      expect(saveButton(section('Limits'))).toBeNull()

      await user.clear(box)
      await user.type(box, '2')
      expect(saveButton(section('Limits'))).toBeInTheDocument()
      await user.click(within(section('Limits')).getByRole('button', { name: 'Reset' }))
      expect(box).toHaveValue(1)
      expect(saveButton(section('Limits'))).toBeNull()

      await user.clear(box)
      await user.type(box, '1') // typed back to the saved value: nothing to save
      expect(saveButton(section('Limits'))).toBeNull()
    })

    it('saves only the field that was changed, on the device', async () => {
      const { fn, calls } = stubFetch({ 'PATCH /devices/antenna': () => ({ body: {} }), ...okState })
      vi.stubGlobal('fetch', fn)
      const user = userEvent.setup()
      renderSettings(state(), 'access')

      const box = screen.getByLabelText('Tuners reserved for you')
      await user.clear(box)
      await user.type(box, '2')
      await user.click(saveButton(section('Limits'))!)
      await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')).toBeDefined())
      expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ ownerReserve: 2 })
    })

    it('sends zero for "no cap"', async () => {
      const { fn, calls } = stubFetch({ 'PATCH /devices/antenna': () => ({ body: {} }), ...okState })
      vi.stubGlobal('fetch', fn)
      const user = userEvent.setup()
      renderSettings(state(), 'access')
      await user.clear(screen.getByLabelText('Max tuners for viewers'))
      await user.type(screen.getByLabelText('Max tuners for viewers'), '0')
      await user.click(saveButton(section('Limits'))!)
      await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ sharedCap: 0 }))
    })

    it('rejects nonsense locally, including an emptied box that would quietly become 0', async () => {
      const { fn, calls } = stubFetch({})
      vi.stubGlobal('fetch', fn)
      const user = userEvent.setup()
      renderSettings(state(), 'access')

      for (const bad of ['-1', '1.5', '']) {
        const input = screen.getByLabelText('Tuners reserved for you')
        await user.clear(input)
        if (bad) await user.type(input, bad)
        await user.click(saveButton(section('Limits'))!)
        expect(await within(section('Limits')).findByText('Enter a whole number, 0 or more.')).toBeInTheDocument()
      }
      expect(calls).toHaveLength(0)
    })
  })

  describe('access', () => {
    it('parses the admin list into addresses', async () => {
      const { fn, calls } = stubFetch({ 'PATCH /settings': () => ({ body: {} }), ...okState })
      vi.stubGlobal('fetch', fn)
      const user = userEvent.setup()
      renderSettings(state(), 'access')

      const box = screen.getByLabelText('Admin addresses')
      await user.clear(box)
      await user.type(box, '192.168.1.5{Enter}10.0.0.0/8') // Enter makes a new line in a multi-line box
      await user.click(saveButton(section('Admin access'))!)
      await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ admin: ['192.168.1.5', '10.0.0.0/8'] }))
    })

    it('can empty the admin list', async () => {
      const { fn, calls } = stubFetch({ 'PATCH /settings': () => ({ body: {} }), ...okState })
      vi.stubGlobal('fetch', fn)
      const user = userEvent.setup()
      renderSettings(state(), 'access')
      await user.clear(screen.getByLabelText('Admin addresses'))
      await user.click(saveButton(section('Admin access'))!)
      await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ admin: [] }))
    })

    it("shows the server's reason when it refuses an address", async () => {
      const { fn } = stubFetch({ 'PATCH /settings': () => ({ status: 400, body: { error: 'admin: bad address "nope"' } }) })
      vi.stubGlobal('fetch', fn)
      const user = userEvent.setup()
      renderSettings(state(), 'access')
      await user.clear(screen.getByLabelText('Admin addresses'))
      await user.type(screen.getByLabelText('Admin addresses'), 'nope')
      await user.click(saveButton(section('Admin access'))!)
      expect(await within(section('Admin access')).findByRole('alert')).toHaveTextContent('admin: bad address "nope"')
    })
  })

  describe('firmware check', () => {
    it('is a switch that takes effect when flipped', async () => {
      const { fn, calls } = stubFetch({ 'PATCH /settings': () => ({ body: {} }), ...okState })
      vi.stubGlobal('fetch', fn)
      const user = userEvent.setup()
      renderSettings(state(), 'privacy')

      await user.click(screen.getByRole('switch', { name: 'Check for new tuner firmware' }))
      await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')).toBeDefined())
      expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ firmwareCheck: false })
    })

    it('shows it off, and turns it on', async () => {
      const { fn, calls } = stubFetch({ 'PATCH /settings': () => ({ body: {} }), ...okState })
      vi.stubGlobal('fetch', fn)
      const user = userEvent.setup()
      const s = state()
      s.settings.firmwareCheck = false
      renderSettings(s, 'privacy')

      expect(screen.getByRole('switch', { name: 'Check for new tuner firmware' })).not.toBeChecked()
      await user.click(screen.getByRole('switch', { name: 'Check for new tuner firmware' }))
      await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ firmwareCheck: true }))
    })

    it('says what it sends: nothing about you', async () => {
      renderSettings(state(), 'privacy')
      const tip = await explain(section('Privacy and updates'), 'Check for new tuner firmware')
      expect(tip.getByText(/Nothing about you or your tuners is sent/)).toBeInTheDocument()
      expect(tip.getByText(/installs one only when you press Install on a tuner's Firmware tab/)).toBeInTheDocument()
    })

    it('shows a failure on the row', async () => {
      const { fn } = stubFetch({ 'PATCH /settings': () => ({ status: 500, body: { error: 'could not save the settings' } }) })
      vi.stubGlobal('fetch', fn)
      const user = userEvent.setup()
      renderSettings(state(), 'privacy')
      await user.click(screen.getByRole('switch', { name: 'Check for new tuner firmware' }))
      expect(await within(section('Privacy and updates')).findByRole('alert')).toHaveTextContent('could not save the settings')
    })
  })

  it('cannot be edited when there is no settings file', async () => {
    const user = userEvent.setup()
    const { unmount } = renderSettings({ ...state(), editable: false }, 'privacy')
    expect(screen.getByRole('switch', { name: 'Check for new tuner firmware' })).toHaveAttribute('aria-disabled', 'true')
    unmount()
    renderSettings({ ...state(), editable: false }, 'access')
    for (const label of ['Tuners reserved for you', 'Max tuners for viewers', 'Admin addresses']) expect(screen.getByLabelText(label)).toBeDisabled()
    await user.type(screen.getByLabelText('Tuners reserved for you'), '1') // typing into a disabled box does nothing
    expect(saveButton(section('Limits'))).toBeNull()
  })

  describe('with several devices', () => {
    it("has a section for each, titled with the name it was given, each showing its own values", () => {
      const s = makeTwoDeviceState()
      s.devices[0].sharedCap = 2
      s.devices[1] = makeDevice({ ...s.devices[1], label: 'Cable box' })
      renderSettings(s, 'access')

      const antenna = section('Limits: Steve FLEX 4K')
      const cable = section('Limits: Cable box')
      expect(within(antenna).getByLabelText('Tuners reserved for you')).toHaveValue(1)
      expect(within(antenna).getByLabelText('Max tuners for viewers')).toHaveValue(2)
      expect(within(cable).getByLabelText('Tuners reserved for you')).toHaveValue(0)
      expect(within(cable).getByText(/Cable HDHR · 2 tuners/)).toBeInTheDocument()
    })

    it("saves a device's value on that device only", async () => {
      const { fn, calls } = stubFetch({ 'PATCH /devices/cable': () => ({ body: {} }), ...okState })
      vi.stubGlobal('fetch', fn)
      const user = userEvent.setup()
      renderSettings(makeTwoDeviceState(), 'access')

      const cable = section('Limits: Cable HDHR')
      const box = within(cable).getByLabelText('Tuners reserved for you')
      await user.clear(box)
      await user.type(box, '1')
      await user.click(saveButton(cable)!)
      await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')).toBeDefined())
      expect(calls.find((c) => c.method === 'PATCH')).toMatchObject({ url: expect.stringContaining('/devices/cable'), body: { ownerReserve: 1 } })
    })
  })

  it('keeps the limits section plainly titled for a single tuner, whatever it is named', () => {
    const s = state()
    s.devices[0].label = 'Living room antenna'
    renderSettings(s, 'access')
    expect(section('Limits')).toBeInTheDocument()
  })
})

describe('guide setting', () => {
  it('is on by default and can be switched off, saying what it sends', async () => {
    const { fn, calls } = stubFetch({ 'PATCH /settings': () => ({ body: {} }), ...okState })
    vi.stubGlobal('fetch', fn)
    renderSettings(state(), 'privacy')
    const sw = within(section('Privacy and updates')).getByRole('switch', { name: "Show what's on" })
    expect(sw).toBeChecked()
    expect((await explain(section('Privacy and updates'), "Show what's on")).getByText(/sends SiliconDust your tuner's guide key/)).toBeInTheDocument()
    await userEvent.setup().click(sw)
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ guide: false }))
  })

  it('cannot be changed from a read-only view', () => {
    const s = state()
    s.editable = false
    renderSettings(s, 'privacy')
    expect(within(section('Privacy and updates')).getByRole('switch', { name: "Show what's on" })).toHaveAttribute('aria-disabled', 'true')
  })
})

describe('activity history setting', () => {
  const privacy = () => within(section('Privacy and updates'))

  it('is on by default and can be switched off, saying what it keeps', async () => {
    const { fn, calls } = stubFetch({ 'PATCH /settings': () => ({ body: {} }), ...okState })
    vi.stubGlobal('fetch', fn)
    renderSettings(state(), 'privacy')
    const sw = privacy().getByRole('switch', { name: 'Keep activity history' })
    expect(sw).toBeChecked()
    expect((await explain(privacy(), 'Keep activity history')).getByText(/A viewer's address is not kept/)).toBeInTheDocument()
    await userEvent.setup().click(sw)
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ history: false }))
  })

  it('cannot be switched from a read-only view', () => {
    const s = state()
    s.editable = false
    renderSettings(s, 'privacy')
    expect(privacy().getByRole('switch', { name: 'Keep activity history' })).toHaveAttribute('aria-disabled', 'true')
  })

  it('can be cleared, after asking, and sends nothing until you agree', async () => {
    const { fn, calls } = stubFetch({ 'DELETE /history': () => ({ status: 204 }), ...okState })
    vi.stubGlobal('fetch', fn)
    renderSettings(state(), 'privacy')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Clear' }))
    const d = await screen.findByRole('alertdialog')
    expect(within(d).getByText('Clear the activity history?')).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'DELETE')).toHaveLength(0)

    await user.click(within(d).getByRole('button', { name: 'Clear' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/history'))).toBe(true))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
  })

  it('can be cancelled', async () => {
    const { fn, calls } = stubFetch({ 'DELETE /history': () => ({ status: 204 }), ...okState })
    vi.stubGlobal('fetch', fn)
    renderSettings(state(), 'privacy')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Clear' }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: /Keep it/ }))
    expect(calls.filter((c) => c.method === 'DELETE')).toHaveLength(0)
  })
})

describe('Plex sign-in settings', () => {
  const withPlex = (admins: { id: number; name: string }[] = [], enabled = true) => {
    const s = state()
    s.settings.plex = { enabled, admins }
    return s
  }
  const access = () => section('Admin access')

  it('is off by default and shows no accounts list', () => {
    renderSettings(withPlex([], false), 'access')
    expect(within(access()).getByRole('switch', { name: 'Sign in with Plex' })).not.toBeChecked()
    expect(screen.queryByText('Allowed Plex accounts')).not.toBeInTheDocument()
  })

  it('turns on with the switch', async () => {
    const { fn, calls } = stubFetch({ 'PATCH /settings': () => ({ body: {} }), ...okState })
    vi.stubGlobal('fetch', fn)
    renderSettings(withPlex([], false), 'access')
    await userEvent.setup().click(within(access()).getByRole('switch', { name: 'Sign in with Plex' }))
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ plexEnabled: true }))
  })

  it('lists the allowed accounts, or says there are none', () => {
    const { unmount } = renderSettings(withPlex(), 'access')
    expect(screen.getByText('No accounts yet.')).toBeInTheDocument()
    unmount()
    renderSettings(withPlex([{ id: 1, name: 'john' }, { id: 2, name: 'steve' }]), 'access')
    expect(screen.getByText('john')).toBeInTheDocument()
    expect(screen.getByText('steve')).toBeInTheDocument()
    expect(screen.queryByText('No accounts yet.')).not.toBeInTheDocument()
  })

  it('removes an account', async () => {
    const { fn, calls } = stubFetch({ 'DELETE /plex/admins/2': () => ({ status: 204 }), ...okState })
    vi.stubGlobal('fetch', fn)
    renderSettings(withPlex([{ id: 1, name: 'john' }, { id: 2, name: 'steve' }]), 'access')
    await userEvent.setup().click(screen.getByRole('button', { name: 'Remove steve' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/plex/admins/2'))).toBe(true))
    expect(await screen.findByText('steve can no longer sign in')).toBeInTheDocument()
  })

  it('shows why removing failed', async () => {
    vi.stubGlobal('fetch', stubFetch({ 'DELETE /plex/admins/1': () => ({ status: 409, body: { error: 'that is the last admin' } }) }).fn)
    renderSettings(withPlex([{ id: 1, name: 'john' }]), 'access')
    await userEvent.setup().click(screen.getByRole('button', { name: 'Remove john' }))
    expect(await screen.findByText('that is the last admin')).toBeInTheDocument()
  })

  it('adds an account through a Plex sign-in', async () => {
    const { fn, calls } = stubFetch({
      'POST /auth/plex/add/start': () => ({ body: { pinId: 9, authUrl: 'https://app.plex.tv/auth#?code=z' } }),
      'POST /auth/plex/add/poll': () => ({ body: { status: 'ok', account: { id: 5, name: 'newbie' } } }),
      ...okState,
    })
    vi.stubGlobal('fetch', fn)
    const win = fakeWindow()
    stubOpen(win)
    renderSettings(withPlex(), 'access')
    await userEvent.setup().click(screen.getByRole('button', { name: 'Add a Plex account' }))
    expect(win.location.href).toBe('https://app.plex.tv/auth#?code=z')
    expect(await screen.findByText('newbie can now sign in.', {}, { timeout: 6000 })).toBeInTheDocument()
    expect(calls.map((c) => c.url.replace(/^.*?(?=\/auth\/)/, ''))).toContain('/auth/plex/add/poll')
  }, 15000)

  it('cannot be changed from a read-only view', () => {
    const s = withPlex([{ id: 1, name: 'john' }])
    s.editable = false
    renderSettings(s, 'access')
    expect(screen.getByRole('button', { name: 'Remove john' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Add a Plex account' })).toBeDisabled()
    expect(within(access()).getByRole('switch', { name: 'Sign in with Plex' })).toHaveAttribute('aria-disabled', 'true')
  })
})

describe('reachedAnotherWay', () => {
  const status: TailscaleStatus = { built: true, state: 'running', hostname: 'yagi', dnsName: 'Yagi.tail1.ts.net', addresses: ['100.64.0.5'] }
  it('warns for a LAN address, not for the machine itself or the tailnet', () => {
    expect(reachedAnotherWay(status, '192.168.1.5')).toBe(true)
    expect(reachedAnotherWay(status, 'localhost')).toBe(false)
    expect(reachedAnotherWay(status, '127.0.0.1')).toBe(false)
    expect(reachedAnotherWay(status, 'yagi.tail1.ts.net')).toBe(false)
    expect(reachedAnotherWay(status, '100.64.0.5')).toBe(false)
  })
})

describe('Tailscale settings', () => {
  const access = () => section('Tailscale')
  const withTailscale = (over: Partial<NonNullable<ReturnType<typeof state>['tailscale']>> & { enabled?: boolean; hostname?: string; tailnetOnly?: boolean; httpsOn?: boolean }) => {
    const s = state()
    const { enabled = true, hostname = 'yagi', tailnetOnly: tailnetOnlyOn = false, httpsOn = false, ...status } = over
    s.settings.tailscale = { enabled, hostname, tailnetOnly: tailnetOnlyOn, https: httpsOn }
    s.tailscale = { built: true, state: enabled ? 'starting' : 'off', hostname, ...status }
    return s
  }
  const patched = () => stubFetch({ 'PATCH /settings': () => ({ body: {} }), ...okState })

  it('says so, and offers no switch, in a build without the node', () => {
    const s = state()
    s.tailscale = { built: false, state: 'unavailable', hostname: 'yagi' }
    renderSettings(s, 'access')
    expect(within(access()).queryByRole('switch', { name: 'Join your Tailscale network' })).not.toBeInTheDocument()
    expect(within(access()).getByText(/include Tailscale/)).toBeInTheDocument()
  })

  it('says which ports it answers on once connected, and nothing about ports before that', () => {
    const { unmount } = renderSettings(withTailscale({ state: 'running', dnsName: 'attic.example.ts.net', addresses: ['100.64.0.7'], serving: ['5004', '5005'] }), 'access')
    expect(within(access()).getByText(/answering on ports 5004, 5005/)).toBeInTheDocument()
    unmount()
    const one = withTailscale({ state: 'running', dnsName: 'attic.example.ts.net', addresses: ['100.64.0.7'], serving: ['5004'] })
    const { unmount: u2 } = renderSettings(one, 'access')
    expect(within(access()).getByText(/answering on port 5004\./)).toBeInTheDocument()
    u2()
    renderSettings(withTailscale({ state: 'running', dnsName: 'attic.example.ts.net', addresses: ['100.64.0.7'] }), 'access')
    expect(within(access()).queryByText(/answering on/)).not.toBeInTheDocument()
    expect(within(access()).getByText(/^Connected\. Share this device/)).toBeInTheDocument()
  })

  it('has its own section, headed by the Tailscale logo as a decorative picture, with or without the node', () => {
    const { unmount } = renderSettings(withTailscale({ enabled: false }), 'access')
    const logo = access().querySelector('img[src$="tailscale-icon.svg"]')
    expect(logo).not.toBeNull()
    expect(logo).toHaveAttribute('aria-hidden', 'true')
    expect(logo).toHaveAttribute('alt', '')
    expect(logo!.className).toContain('dark:invert') // black on the light theme, white on the dark one
    unmount()
    const s = state()
    s.tailscale = { built: false, state: 'unavailable', hostname: 'yagi' }
    renderSettings(s, 'access')
    expect(access().querySelector('img[src$="tailscale-icon.svg"]')).not.toBeNull()
  })

  it('turns on with the switch, and shows nothing more while it is off', async () => {
    const { fn, calls } = patched()
    vi.stubGlobal('fetch', fn)
    renderSettings(withTailscale({ enabled: false }), 'access')
    expect(screen.queryByText('Name on the tailnet')).not.toBeInTheDocument()
    await userEvent.setup().click(within(access()).getByRole('switch', { name: 'Join your Tailscale network' }))
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ tailscaleEnabled: true }))
  })

  it('offers the sign-in link while the node waits for one', () => {
    renderSettings(withTailscale({ state: 'needs-login', loginUrl: 'https://login.tailscale.com/a/abc' }), 'access')
    const link = within(access()).getByRole('link', { name: /Sign in to Tailscale/ })
    expect(link).toHaveAttribute('href', 'https://login.tailscale.com/a/abc')
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  it('shows the name and address once connected, and saves a new hostname', async () => {
    const { fn, calls } = patched()
    vi.stubGlobal('fetch', fn)
    renderSettings(withTailscale({ state: 'running', dnsName: 'yagi.tail1234.ts.net', addresses: ['100.64.0.5'] }), 'access')
    expect(within(access()).getByText('yagi.tail1234.ts.net')).toBeInTheDocument()
    expect(within(access()).getByText('100.64.0.5')).toBeInTheDocument()
    const user = userEvent.setup()
    const box = within(access()).getByLabelText('Name on the tailnet')
    await user.clear(box)
    await user.type(box, 'attic')
    await user.click(saveButton(access())!)
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ tailscaleHostname: 'attic' }))
  })

  it('offers HTTPS for the admin page once connected, saying what it needs and costs, and saves the choice', async () => {
    const { fn, calls } = patched()
    vi.stubGlobal('fetch', fn)
    renderSettings(withTailscale({ state: 'running', dnsName: 'yagi.tail1234.ts.net' }), 'access')
    const tip = await explain(access(), 'HTTPS for the admin page')
    expect(tip.getByText(/public certificate log/)).toBeInTheDocument()
    expect(tip.getByText(/HTTPS certificates turned on/)).toBeInTheDocument()
    await userEvent.setup().click(within(access()).getByRole('switch', { name: 'HTTPS for the admin page' }))
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ tailscaleHttps: true }))
  })

  it('links to the HTTPS address once it is up, and shows Tailscale’s reason when it is not', () => {
    const { unmount } = renderSettings(withTailscale({ state: 'running', httpsOn: true, https: 'https://yagi.tail1234.ts.net/' }), 'access')
    expect(within(access()).getByRole('link', { name: 'https://yagi.tail1234.ts.net/' })).toHaveAttribute('href', 'https://yagi.tail1234.ts.net/')
    unmount()
    renderSettings(withTailscale({ state: 'running', httpsOn: true, httpsError: 'this tailnet does not have HTTPS enabled' }), 'access')
    expect(within(access()).getByText(/does not have HTTPS enabled/)).toBeInTheDocument()
  })

  it('does not offer HTTPS while the node is not connected', () => {
    renderSettings(withTailscale({ state: 'needs-login' }), 'access')
    expect(within(access()).queryByRole('switch', { name: 'HTTPS for the admin page' })).not.toBeInTheDocument()
  })

  it('refuses a bad hostname before sending it', async () => {
    renderSettings(withTailscale({ state: 'running' }), 'access')
    const user = userEvent.setup()
    const box = within(access()).getByLabelText('Name on the tailnet')
    await user.clear(box)
    await user.type(box, 'my yagi')
    await user.click(saveButton(access())!)
    expect(await within(access()).findByText(/not starting or ending with a hyphen/i)).toBeInTheDocument()
  })

  it('offers "only on the tailnet" once connected, asks first, and saves only after the person agrees', async () => {
    const { fn, calls } = patched()
    vi.stubGlobal('fetch', fn)
    const s = withTailscale({ state: 'running', dnsName: 'yagi.tail1234.ts.net', addresses: ['100.64.0.5'], adminReachable: false }) // nobody on the tailnet could open the page afterwards
    renderSettings(s, 'access')
    const user = userEvent.setup()
    await user.click(within(access()).getByRole('switch', { name: 'Only on the tailnet' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText(/refused/)).toBeInTheDocument()
    expect(within(dialog).getByRole('alert')).toHaveTextContent(/Nobody on the tailnet is allowed/) // no admin address covers it
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false) // nothing yet
    await user.click(within(dialog).getByRole('button', { name: 'Close the LAN' }))
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ tailnetOnly: true }))
  })

  it('does not warn about admin addresses when the tailnet is already allowed', async () => {
    renderSettings(withTailscale({ state: 'running', dnsName: 'yagi.tail1234.ts.net', addresses: ['100.64.0.5'], adminReachable: true }), 'access')
    await userEvent.setup().click(within(access()).getByRole('switch', { name: 'Only on the tailnet' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
  })

  it('does not offer it while the node is not connected, and turns it off without asking', async () => {
    const { unmount } = renderSettings(withTailscale({ state: 'needs-login' }), 'access')
    expect(within(access()).queryByRole('switch', { name: 'Only on the tailnet' })).not.toBeInTheDocument()
    unmount()
    const { fn, calls } = patched()
    vi.stubGlobal('fetch', fn)
    renderSettings(withTailscale({ state: 'starting', tailnetOnly: true }), 'access') // on, but the node is not connected: still there to switch off
    expect(within(access()).getByText(/not in force/)).toBeInTheDocument()
    await userEvent.setup().click(within(access()).getByRole('switch', { name: 'Only on the tailnet' }))
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ tailnetOnly: false }))
  })

  it('tells the admin addresses row that LAN addresses no longer matter while the LAN is closed', () => {
    renderSettings(withTailscale({ state: 'running', tailnetOnly: true, lanClosed: true }), 'access')
    expect(within(section('Admin access')).getByText(/addresses on your LAN can’t connect at all/)).toBeInTheDocument()
  })

  it('says when the LAN is closed', () => {
    renderSettings(withTailscale({ state: 'running', tailnetOnly: true, lanClosed: true }), 'access')
    expect(within(access()).getByRole('switch', { name: 'Only on the tailnet' })).toBeChecked()
    expect(within(access()).getByText(/own address is refused/)).toBeInTheDocument()
  })
})
