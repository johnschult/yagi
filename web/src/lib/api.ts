import type { AllowInput, AppState, ChannelInput, ChannelsDoc, ChannelState, CheckReport, DeviceInfo, DeviceInput, HistoryDoc, LogLevel, LogsDoc, SignalTestResult, TunerChannel, FirewallInfo, FoundTuner, NetworkAddress, AuthStatus, DevicePatch, DeviceState, PlexPollStatus, NotifyChannel, Settings, SettingsPatch, UpgradeRequest, Viewer, ViewerInput } from './types'

// The UI is served from / and the API lives beside it.
export const API = `${import.meta.env.BASE_URL}api`

/** An error reply from the Go API; message is safe to show to the user. */
export class ApiError extends Error {
  readonly status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

// Signing in lives beside the API, not inside it: it has to work before you are an admin.
export const AUTH = `${import.meta.env.BASE_URL}auth`

async function request<T>(method: string, path: string, body?: unknown, base: string = API): Promise<T> {
  let res: Response
  try {
    res = await fetch(`${base}${path}`, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'Could not reach Yagi')
  }
  if (res.status === 204) return undefined as T

  const text = await res.text()
  let data: unknown
  try {
    data = text ? JSON.parse(text) : undefined
  } catch {
    data = undefined
  }
  if (!res.ok) {
    const msg = (data as { error?: string } | undefined)?.error
    throw new ApiError(res.status, msg ?? `Request failed (${res.status})`)
  }
  return data as T
}

export const getState = () => request<AppState>('GET', '/state')
export const addViewer = (v: ViewerInput) => request<Viewer>('POST', '/viewers', v)
export const replaceViewer = (name: string, v: ViewerInput) =>
  request<Viewer>('PUT', `/viewers/${encodeURIComponent(name)}`, v)
export const deleteViewer = (name: string) => request<void>('DELETE', `/viewers/${encodeURIComponent(name)}`)
/** Allow someone who asked: they become a viewer, recognized by who they are. */
export const allowRequest = (id: string, i: AllowInput) => request<Viewer>('POST', `/requests/${encodeURIComponent(id)}/allow`, i)
export const dismissRequest = (id: string) => request<void>('DELETE', `/requests/${encodeURIComponent(id)}`)
export const patchSettings = (p: SettingsPatch) => request<Settings>('PATCH', '/settings', p)
export const patchDevice = (name: string, p: DevicePatch) =>
  request<DeviceState>('PATCH', `/devices/${encodeURIComponent(name)}`, p)

/** Search the network for tuners now. Takes a few seconds. */
export const discoverTuners = () => request<{ tuners: FoundTuner[] }>('POST', '/discover').then((r) => r.tuners)
export const addDevice = (d: DeviceInput) => request<DeviceState>('POST', '/devices', d)
export const deleteDevice = (name: string) => request<void>('DELETE', `/devices/${encodeURIComponent(name)}`)

/** Read SiliconDust's changelog now. The new status arrives over the event stream. */
export const checkFirmware = () => request<{ checkedAt: string }>('POST', '/firmware/check')

export const addChannel = (c: ChannelInput) => request<NotifyChannel>('POST', '/notifications/channels', c)
export const replaceChannel = (id: string, c: ChannelInput) => request<NotifyChannel>('PUT', `/notifications/channels/${encodeURIComponent(id)}`, c)
export const deleteChannel = (id: string) => request<void>('DELETE', `/notifications/channels/${encodeURIComponent(id)}`)
/** Send one test message to a channel: a saved one by `id`, or the form's values, or a saved one with edits not yet saved. */
export const testChannel = (t: { id?: string; channel?: ChannelInput }) => request<{ ok: true }>('POST', '/notifications/test', t)

/** Install the latest firmware on a tuner. It runs in the background: progress is in the state. */
export const installFirmware = (device: string, r: UpgradeRequest) =>
  request<{ release: string; from: string }>('POST', `/devices/${encodeURIComponent(device)}/firmware/upgrade`, r)
/** Clear a finished install's result. */
export const setDiagnostics = (on: boolean, minutes?: number) => request<{ on: boolean; until?: string }>('PUT', '/diagnostics', { on, minutes })
/** A report on how Yagi and its tuners are doing now, as text to read, copy and send. */
export const makeReport = () => request<{ at: string; text: string }>('POST', '/report')
export const checkTuner = (device: string) => request<CheckReport>('POST', `/devices/${encodeURIComponent(device)}/check`)
export const getDeviceInfo = (device: string, refresh = false) =>
  request<DeviceInfo>('GET', `/devices/${encodeURIComponent(device)}/info${refresh ? '?refresh=1' : ''}`)
export const restartTuner = (device: string, interrupt: boolean) =>
  request<void>('POST', `/devices/${encodeURIComponent(device)}/restart`, { interrupt })
export const dismissRestart = (device: string) => request<void>('DELETE', `/devices/${encodeURIComponent(device)}/restart`)
export const clearHistory = () => request<void>('DELETE', '/history')
/** The recent log lines: at or above `level`, containing `q`, the newest `limit`. */
export const getLogs = (level: LogLevel | '', q: string, limit = 500) => {
  const p = new URLSearchParams({ limit: String(limit) })
  if (level) p.set('level', level)
  if (q.trim()) p.set('q', q.trim())
  return request<LogsDoc>('GET', `/logs?${p}`)
}
export const clearLogs = () => request<void>('DELETE', '/logs')
export const getChannels = (device: string) => request<ChannelsDoc>('GET', `/devices/${encodeURIComponent(device)}/channels`)
export const setChannelState = (device: string, number: string, state: ChannelState) =>
  request<TunerChannel>('PUT', `/devices/${encodeURIComponent(device)}/channels/${encodeURIComponent(number)}`, { state })
/** Tune a free tuner to a channel for a few seconds and read its signal. Takes about ten seconds. */
export const testChannelSignal = (device: string, number: string) =>
  request<SignalTestResult>('POST', `/devices/${encodeURIComponent(device)}/channels/${encodeURIComponent(number)}/test`)
export const startChannelScan = (device: string, source: string, interrupt: boolean) =>
  request<{ scanning: boolean; source: string }>('POST', `/devices/${encodeURIComponent(device)}/channel-scan`, { source, interrupt })
export const stopChannelScan = (device: string) => request<void>('DELETE', `/devices/${encodeURIComponent(device)}/channel-scan`)
export const getHistory = (days: number) => request<HistoryDoc>('GET', `/history?days=${days}`)
export const dismissUpgrade = (device: string) => request<void>('DELETE', `/devices/${encodeURIComponent(device)}/firmware/upgrade`)

export const removePlexAdmin = (id: number) => request<void>('DELETE', `/plex/admins/${id}`)

export const getAuthStatus = () => request<AuthStatus>('GET', '/status', undefined, AUTH)
export const startPlex = (add = false) =>
  request<{ pinId: number; authUrl: string }>('POST', `/plex${add ? '/add' : ''}/start`, {}, AUTH)
export const pollPlex = (pinId: number, add = false) =>
  request<PlexPollStatus>('POST', `/plex${add ? '/add' : ''}/poll`, { pinId }, AUTH)
export const signOut = () => request<unknown>('POST', '/logout', undefined, AUTH)

/** Where a channel's logo is served from: through Yagi, never straight from SiliconDust. */
export const channelIconUrl = (device: string, channel: string) =>
  `${API}/devices/${encodeURIComponent(device)}/guide/${encodeURIComponent(channel)}/icon`

/** Where a program's picture is served from: through Yagi, never straight from SiliconDust. The token is the guide's `now.poster`. */
export const programPosterUrl = (device: string, channel: string, token: string) =>
  `${API}/devices/${encodeURIComponent(device)}/guide/${encodeURIComponent(channel)}/poster/${encodeURIComponent(token)}`

/** This machine's Tailscale and LAN addresses and its firewall, for the setup guide. */
export const getNetwork = () => request<{ addresses: NetworkAddress[]; firewall?: FirewallInfo }>('GET', '/network')
