// Shapes of the Go API (internal/api). Keep in step with api.go.

export interface Slot {
  resource: string
  inUse: boolean
  channel?: string
  channelName?: string
  targetIP?: string
  viaProxy: boolean
  signalStrength?: number
  signalQuality?: number
  bitsPerSecond?: number
}

export type EventKind = 'ok' | 'info' | 'warn' | 'err'

export interface TunerEvent {
  time: string
  kind: EventKind
  text: string
}

/** What the tuner itself says about its firmware. */
export interface TunerFirmware {
  name?: string
  version?: string
  upgradeAvailable?: string
}

export interface FirmwareNote {
  scope?: string
  text: string
}

export interface FirmwareUpdate {
  version: string
  /** True when a note is plainly for this model; false when it only might be. */
  certain: boolean
  notes: FirmwareNote[]
}

export type FirmwareState = 'up-to-date' | 'update-available' | 'possible-update' | 'unknown' | 'disabled'

/** Where a tuner's firmware stands against SiliconDust's changelog. */
export interface FirmwareStatus {
  state: FirmwareState
  current: string
  latest: string
  deviceReported?: string
  updates: FirmwareUpdate[]
  modelRecognized: boolean
  /** Zero time ("0001-01-01...") when it has never been read. */
  checkedAt: string
  error?: string
}

/** The last time Yagi went looking for a tuner that wasn't answering. */
export interface TunerSearch {
  at: string
  found: boolean
}

export type ProblemKind = 'refused' | 'no-channels' | 'denied'

/** Trouble a media server ran into, which it only shows as "channel not available" or an empty guide. The latest of each kind. */
export interface TunerProblem {
  kind: ProblemKind
  /** When this episode of it started. It stays the same while the trouble goes on (a media server retries every few seconds). */
  time: string
  /** The latest time it happened. */
  last: string
  text: string
}

export interface TunerState {
  name?: string
  online: boolean
  id: string
  friendlyName?: string
  model?: string
  baseURL: string
  tunerCount: number
  firmware: TunerFirmware
  slots: Slot[]
  lastSeen: string
  since: string
  events: TunerEvent[]
  /** Set while the tuner isn't answering and Yagi has gone looking for it. */
  search?: TunerSearch
  problems?: TunerProblem[]
  /** Yagi is installing firmware: the tuner is expected to go away and come back, so it is not offline. */
  updating: boolean
  /** Yagi is restarting the tuner for someone: it will go away and come back, so it is not offline. */
  restarting: boolean
}

export type RestartPhase = 'restarting' | 'done' | 'failed'

/** A tuner restart someone asked for: while it runs, and how it ended. */
export interface RestartStatus {
  phase: RestartPhase
  error?: string
  started: string
  finished?: string
}

export type UpgradePhase = 'downloading' | 'sending' | 'restarting' | 'verifying' | 'done' | 'failed'

/** A firmware install on a tuner, while it runs and after. */
export interface UpgradeStatus {
  phase: UpgradePhase
  /** The version being installed, and what the tuner ran before. */
  release: string
  from: string
  /** How much of the image has been sent, 0 to 100. */
  percent: number
  /** Why it failed, in words for a person. */
  error?: string
  sha256?: string
  started: string
  finished?: string
}

/** What is sent to install firmware. */
export interface UpgradeRequest {
  /** The release the page was offering: the server refuses if a newer one has been found since. */
  version: string
  /** The person has been told something is running and that this will cut it off. */
  interrupt?: boolean
}

export interface Stream {
  id: number
  device: string
  viewer: string
  client: string
  path: string
  channel?: string
  started: string
}

export interface Viewer {
  name: string
  addresses: string[]
  /** Tuners at once on a device, unless `limits` says otherwise for it. */
  limit: number
  /** Per-device overrides, by device name. */
  limits?: Record<string, number>
  inUse: number
  inUseByDevice: Record<string, number>
  lastSeen: string | null
  /** Addresses in this viewer's list that are on the public internet. Yagi serves private networks only, so these can never connect. */
  publicAddresses?: string[]
}

/** A Plex account that may sign in to the admin pages. */
export interface PlexAccount {
  id: number
  name: string
}

export interface PlexSettings {
  enabled: boolean
  admins: PlexAccount[]
}

/** Who this browser is, as far as signing in goes. Public: it is how the page knows whether to offer sign-in. */
export interface AuthStatus {
  plexEnabled: boolean
  signedIn: { id: number; name: string } | null
  /** Whether this request may use the admin pages at all (by address, or by being signed in). */
  admin: boolean
}

export type PlexPollStatus =
  | { status: 'pending' }
  | { status: 'expired' }
  | { status: 'ok'; user?: { id: number; name: string }; account?: { id: number; name: string } }
  | { status: 'denied'; user: { name: string } }

export type ChannelType = 'ntfy' | 'pushover' | 'webhook'

/**
 * A place notifications go. Its secrets are not here: the server says whether it
 * has them, never what they are, and a webhook's address (which often holds one) is
 * shown as its host.
 */
export interface NotifyChannel {
  id: string
  name: string
  type: ChannelType
  /** The ntfy server. */
  url?: string
  /** A webhook's host. */
  host?: string
  topic?: string
  /** A ntfy or webhook token, or the Pushover application token, is saved. */
  hasToken: boolean
  /** The Pushover user key is saved. */
  hasUser: boolean
}

export type NotifyEvent = 'firmware' | 'tuner' | 'viewer' | 'unknown' | 'watching' | 'request'
export type NotifyEvents = Record<NotifyEvent, boolean>

export interface Notifications {
  channels: NotifyChannel[]
  events: NotifyEvents
}

/** What is sent to add or change a channel. A secret left empty keeps the saved one. */
export interface ChannelInput {
  name: string
  type: ChannelType
  url?: string
  topic?: string
  token?: string
  user?: string
  /** Remove the saved token (an empty token means "keep it"). */
  clearToken?: boolean
}

export interface Settings {
  mode: 'no-viewers' | 'enforced'
  sharedCap: number
  ownerReserve: number
  admin: string[]
  /** Whether SiliconDust's changelog is read (one request every 12 hours). */
  firmwareCheck: boolean
  guide: boolean
  /** Whether who watched what is being kept, for the Activity page. */
  history: boolean
  /** How much is logged, and shown on the Logs page. */
  logLevel: LogLevel
  plex: PlexSettings
  tailscale: TailscaleSettings
  notifications: Notifications
}

export interface TailscaleSettings {
  enabled: boolean
  /** The name on the tailnet; "yagi" unless changed. */
  hostname: string
  /** Refuse connections from the LAN while the node is connected. */
  tailnetOnly: boolean
  /** Also serve the admin pages over HTTPS at the node's tailnet name. */
  https: boolean
}

/** Where the built-in Tailscale node is. */
export interface TailscaleStatus {
  /** This build has the node at all (the -tailscale image does). */
  built: boolean
  state: 'off' | 'unavailable' | 'no-store' | 'starting' | 'needs-login' | 'running' | 'error'
  hostname: string
  /** The full name the tailnet gave it, once running. */
  dnsName?: string
  /** Where to sign the node in, while state is needs-login. */
  loginUrl?: string
  addresses?: string[]
  error?: string
  /** TS_AUTHKEY is set, so signing in needs no link. */
  authKeyInEnv?: boolean
  /** The ports Yagi answers on at the node's addresses: the tuners' ports, and the admin port when it has its own. */
  serving?: string[]
  /** The LAN is being refused right now: tailnet only is on and the node is connected. */
  lanClosed?: boolean
  /** Somebody on the tailnet could open the admin pages (an admin range covers it, or a Plex account is allowed). */
  adminReachable?: boolean
  /** Where the admin pages are served over HTTPS, once they are. */
  https?: string
  /** Why they are not, when HTTPS is on, in Tailscale's words. */
  httpsError?: string
}

export interface Summary {
  total: number
  inUse: number
  remote: number
  local: number
  reserved: number
  free: number
}

/** One HDHomeRun that Yagi fronts. */
export interface DeviceState {
  /** The stable key: used in settings, limits and URLs. */
  name: string
  /** The display name the owner chose, if any. */
  label?: string
  listen: string
  tuner: TunerState
  streams: Stream[]
  summary: Summary
  /** The values in force for this device (its own, or the defaults). */
  ownerReserve: number
  sharedCap: number
  firmware: FirmwareStatus
  /** The latest firmware install on this tuner, running or finished. Absent when there has been none. */
  upgrade?: UpgradeStatus
  restart?: RestartStatus
  /** What the program guide says about each channel being watched, by channel number. */
  guide: Record<string, GuideEntry>
}

/** A channel as the program guide knows it. */
export interface GuideEntry {
  name?: string
  /** Whether a logo can be fetched (see channelIconUrl). */
  icon: boolean
  /** What is on right now. */
  now?: {
    title: string
    subTitle?: string
    description?: string
    /** Series, News, Movie... */
    category?: string
    /** As shown on screen: S01E32. */
    episode?: string
    start: string
    stop: string
    /** Names the program's picture, for programPosterUrl. Absent when the guide has none. */
    poster?: string
  }
}

export interface AppState {
  time: string
  devices: DeviceState[]
  viewers: Viewer[]
  requests: AccessRequest[]
  settings: Settings
  editable: boolean
  /** The built-in Tailscale node. Absent from an older server. */
  tailscale?: TailscaleStatus
  /** This build's version ("2026.10.0", or "dev") and its page on GitHub. */
  version: string
  versionUrl: string
  /** The documentation as it was at this version. */
  docsUrl: string
}

/** A tuner found on the network by a search. `added` is the device it already is, if any. */
export interface FoundTuner {
  id: string
  address: string
  tuners: number
  added: string
  /** What the tuner calls itself, offered as its display name. Empty when it could not be asked. */
  label: string
  /** A device name to offer, such as flex-4k-87. It shows in addresses, so it is plain. */
  name: string
}

/** A tuner to start sharing. Only the address (or DeviceID) is needed; the rest is chosen. */
export interface DeviceInput {
  tuner: string
  id?: string
  name?: string
  label?: string
  listen?: string
}

export interface ViewerInput {
  name: string
  addresses: string[]
  limit: number
  limits?: Record<string, number>
}

/** Someone who asked to use a tuner and is not a viewer yet. They are shown on the Viewers page, to be allowed. */
export interface AccessRequest {
  id: string
  /** What to call them: their Tailscale login, else their device, else their address. */
  label: string
  address: string
  login?: string
  node?: string
  /** The tuner they asked for, the last time. */
  device: string
  first: string
  last: string
  count: number
}

/** What is sent to allow a request: who they will be called, and what they get. Their address is what Yagi saw. */
export interface AllowInput {
  name: string
  limit: number
  limits?: Record<string, number>
}

export interface SettingsPatch {
  sharedCap?: number
  ownerReserve?: number
  admin?: string[]
  firmwareCheck?: boolean
  guide?: boolean
  history?: boolean
  logLevel?: LogLevel
  plexEnabled?: boolean
  tailscaleEnabled?: boolean
  /** The name on the tailnet; an empty string means the default. */
  tailscaleHostname?: string
  tailnetOnly?: boolean
  tailscaleHttps?: boolean
  notifyEvents?: Partial<NotifyEvents>
}

export interface DevicePatch {
  sharedCap?: number
  ownerReserve?: number
  /** The display name; an empty string clears it. */
  label?: string
  /** Where the tuner is reached, e.g. ":5010". The server opens it before letting go of the old one. */
  listen?: string
}

/** One way to reach the machine running Yagi, read from its network interfaces. */
export interface NetworkAddress {
  ip: string
  kind: 'tailscale' | 'lan'
  interface: string
}

/** Which firewall looks to be in use on the machine running Yagi. Its rules can't be read, so this is only which tool is switched on. */
export interface FirewallInfo {
  tool: '' | 'ufw' | 'firewalld' | 'iptables'
  active: boolean
}

/** What a tuner says about itself (the Device panel). A field that could not be read is missing, and `problems` says why. */
export interface DeviceInfo {
  model?: string
  hardwareModel?: string
  version?: string
  channelMaps?: string[]
  modulations?: string[]
  deviceId?: string
  drm?: string
  mac?: string
  ip?: string
  subnet?: string
  readAt: string
  problems?: string[]
}

/** One thing the Check now button looked at, and what it found, in words. */
export interface CheckStep {
  id: string
  title: string
  status: 'ok' | 'problem' | 'note' | 'skipped'
  detail: string
}

/** What a check found: the steps in order, and what the tuner printed about itself. */
export interface CheckReport {
  at: string
  steps: CheckStep[]
  debug?: { name: string; lines: string[] }[]
}

/** One tuner on one channel for one viewer, from when it began to when it was last seen. */
export interface HistorySession {
  device: string
  tuner: string
  channel: string
  channelName?: string
  kind: 'viewer' | 'remote' | 'local'
  /** For a viewer. */
  viewer?: string
  /** For the owner's own device: its address on their network. */
  client?: string
  start: string
  end: string
  /** The programs that were on, in order. */
  titles?: string[]
  /** About how much the tuner sent for it, in bytes. */
  bytes?: number
  /** The average signal quality, 0 to 100, when the tuner reported one. */
  quality?: number
  /** Not ended yet: `end` is the last time it was seen. */
  open?: boolean
}

export interface HistoryDoc {
  days: number
  now: string
  sessions: HistorySession[]
}

export type ChannelState = 'enabled' | 'favorite' | 'disabled'

/** A channel a tuner has found. */
export interface TunerChannel {
  number: string
  name: string
  hd?: boolean
  video?: string
  audio?: string
  drm?: boolean
  state: ChannelState
  /** False: the tuner has it but can't play it, so it can't be changed. */
  subscribed: boolean
  /** What the program guide says about it as a whole. Absent when the guide doesn't have the channel or is off. */
  about?: ChannelAbout
}

/** What SiliconDust's program guide says about a channel as a whole. Anything it doesn't say is absent: nothing is guessed. */
export interface ChannelAbout {
  /** The network it carries (ABC), when that differs from its name. */
  network?: string
  /** Language codes as the guide gives them (en, es), most programs first. */
  languages?: string[]
  /** What its programs are, most common first. */
  categories?: string[]
}

/** What the tuner says about scanning for channels. */
export interface ChannelScan {
  inProgress: boolean
  possible: boolean
  source?: string
  sources: string[]
  progress?: number
  found?: number
}

export interface ChannelsDoc {
  channels: TunerChannel[]
  scan: ChannelScan
}

/** What a tuner reported while tuned to a channel for a test. */
export interface SignalTestResult {
  tuner: number
  channel: string
  /** Whether the tuner got a lock on it. */
  locked: boolean
  lock?: string
  strength: number
  quality: number
  symbol: number
}

export type LogLevel = 'debug' | 'info' | 'warn' | 'error'

/** One line of Yagi's log. */
export interface LogEntry {
  /** Counts up from 1 for as long as Yagi runs. */
  seq: number
  time: string
  level: LogLevel
  message: string
  attrs?: { key: string; value: string }[]
}

/** The recent log lines that matched, oldest first. */
export interface LogsDoc {
  entries: LogEntry[]
  latest: number
  /** How many lines are held, and how many can be. */
  kept: number
  capacity: number
  /** The level being logged at: lines below it are never kept. */
  level: LogLevel
}
