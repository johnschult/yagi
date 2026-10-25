---
title: "Configuration"
---

Yagi is configured with a **settings file** (JSON), which you can edit by hand or change from the web page, and a few **command-line flags**.

:::tip[You don't need a settings file to start]
Start Yagi with nothing and it creates the file with the defaults, shows the tuners it finds on your network, and saves what you add and change in the web page. This page is for when you would rather write the file yourself, or want to know what each setting does.
:::

- Changes made in the web page are saved to the settings file at once, and take effect at once (except the list of devices, see below).
- The file is read strictly: **a key it doesn't know is an error**, not silently ignored, so a typo can't quietly open access. If the file is wrong, Yagi says why and doesn't start (or, if it is already running and you reload, keeps the settings it has).
- The file is saved atomically and with permissions that only its owner can read.
- Send the process `SIGHUP` (`docker kill -s HUP yagi`) to read the file again after editing it by hand. Changing `devices` needs a restart; everything else is picked up by the reload.

## An example

```json
{
  "devices": [
    { "name": "antenna", "listen": ":5004", "tuner": "192.168.1.50", "id": "10C03B06" }
  ],
  "viewers": [
    { "name": "Mom", "addresses": ["100.64.0.7"], "limit": 1 }
  ],
  "sharedCap": 0,
  "ownerReserve": 1,
  "admin": ["100.64.0.1"],
  "labels": { "antenna": "Living room antenna" },
  "firmwareCheck": true,
  "guide": true,
  "plex": { "enabled": false, "admins": [] },
  "tailscale": { "enabled": false, "hostname": "yagi" },
  "notifications": {
    "channels": [
      { "id": "c1a2b3c4d5", "name": "My phone", "type": "ntfy", "url": "https://ntfy.sh", "topic": "yagi-x7k2q9" }
    ],
    "events": { "unknown": false }
  }
}
```

## Settings

### `devices`

The tuners to front. Each looks like its own HDHomeRun to media servers, on its own port. If you leave `devices` out, Yagi runs one device described by the command-line flags.

| Key | Meaning |
|---|---|
| `name` | A stable key: 1–40 letters, digits, `.`, `_` or `-`. It is used in settings, limits and URLs, so don't change it. |
| `listen` | Where media servers reach this device: `:5004` (every address) or `127.0.0.1:5004`. Each device needs its own port. |
| `tuner` | The tuner's address, as a host or URL (`192.168.1.50`). Where to look first. When Yagi finds the tuner somewhere else (by its `id`), it saves the new address here, so the next start looks in the right place. |
| `id` | The tuner's DeviceID: exactly 8 hex digits. With it, Yagi finds the tuner again if its address changes. If you leave it out, it is learned from the tuner the first time Yagi reaches it. You need `tuner`, `id`, or both. |
| `streamPort` | The tuner's streaming port. Default `5004`. |
| `advertise` | The base URL handed to media servers, for example `http://yagi.example.com:5004`. By default it is whatever host the client used to connect, which is right in almost every case. |
| `reportId` | A different DeviceID (8 hex digits) to show media servers instead of the tuner's own. Use it when a media server already knows the real tuner and would merge the two. |
| `ownerReserve`, `sharedCap` | Override the top-level values below for this device. |

### `viewers`

Who you share with. **With no viewers, nobody can use the tuner**: a new install is closed until you add one.

| Key | Meaning |
|---|---|
| `name` | Required, and unique. |
| `addresses` | At least one. Single addresses or ranges on a private network, a VPN or Tailscale: `100.64.0.7`, `10.8.0.0/24`, `fd7a:115c::/48`, or a Tailscale login. A public internet address is refused by the page and the API, and warned about at start-up if the file has one, because Yagi serves private networks only and it could never connect. The address their requests come from, as Yagi sees it. If ranges overlap, the most specific one wins. An address can't be listed for two viewers. With Yagi's own Tailscale it can also be `tailscale:alice@example.com` (anyone signed in as that login) or `tailscale:laptop.tail1234.ts.net` (that device): see [Who is on the other end](tailscale.md#who-is-on-the-other-end). |
| `limit` | How many tuners they may use at once, on each device. `0` pauses them. |
| `limits` | Overrides `limit` for particular devices, by device name: `{ "antenna": 2 }`. Needs `devices` to be declared in the file. |

### Everything else

| Key | Meaning |
|---|---|
| `sharedCap` | The most tuners *all* viewers together may use on a device. `0` means no cap. |
| `ownerReserve` | Tuners always kept free for your own devices (default `1`), **in addition to** any your own devices are using at the moment. |
| `admin` | Extra addresses (single or ranges) allowed to open the admin pages. Loopback is always allowed once a viewer exists. Keep this narrow, and keep viewers' addresses out of it: a viewer inside an admin range is an admin too (Yagi logs a warning). Also takes `tailscale:` logins and devices, as for a viewer. |
| `labels` | Display names, by device name: `{ "antenna": "Living room antenna" }`, up to 60 characters. It is for the web page only, and changes live. Media servers are always told the device (such as `HDHomeRun FLEX 4K`), so a name you give it here never reaches Plex: renaming doesn't change what Plex shows, and a media server can name its own DVR as it likes. |
| `firmwareCheck` | Look at SiliconDust's firmware changelog every 12 hours (default `true`). |
| `adminListen` | Where the admin pages are: `:9244` (the default, so it is not written) or `127.0.0.1:9244`. A port of its own that no tuner can use. The `-admin-listen` flag and `YAGI_ADMIN_LISTEN` win over it, and a change needs a restart. See [The admin port](#the-admin-port). |
| `logLevel` | How much is logged, and shown on the Logs page: `debug`, `info`, `warn` or `error` (default `info`, and then it is not written). It takes effect at once, also when the file is reloaded. |
| `history` | Keep who watched what for 30 days, for the Activity page (default `true`). Off stops recording and hides the page; what was kept stays until it is cleared (*Settings → Clear activity history*) or the history file is deleted. |
| `guide` | Read SiliconDust's program guide so Live tiles can show the channel logo, what's on and its picture (default `true`). |
| `notifications` | Messages to your phone or another service. `channels` is a list (at most 8); `events` switches kinds of event on or off. See [Notifications](#notifications) below. |
| `tailscale` | The built-in Tailscale node, in the `-tailscale` image: `enabled`, `hostname` (its name on the tailnet, default `yagi`, not written when it is), `tailnetOnly` (refuse everything that arrives over the machine's own address while the node is connected) and `https` (also serve the admin pages over HTTPS at the node's name). Normally set from *Settings → Tailscale*. See [Built-in Tailscale](tailscale.md). |
| `plex` | Sign in to the admin pages with a Plex account. `enabled`, the allowed `admins` (each with an `id` and a `name`, at most 50), and a `clientId` Yagi creates and keeps for you. See [Security](security.md). |

## Notifications

Yagi can message you about things that are worth knowing. Add a channel under **Settings → Notifications** (and press **Test** to check it works), or write it in the file. Nothing is sent, and nothing is contacted, until there is a channel.

Each channel has an `id` (made for you, and never changed), a `name`, and a `type`:

| `type` | Fields | Notes |
|---|---|---|
| `ntfy` | `url` (the server, such as `https://ntfy.sh`), `topic`, and optionally `token` | Sent as JSON to the server. On `ntfy.sh` anyone who knows the topic can read it. |
| `pushover` | `token` (the application token) and `user` (your user key) | Sent to `api.pushover.net`. |
| `webhook` | `url`, and optionally `token` | A JSON `POST` with `event`, `title`, `message`, `device` and `time`. A `token` is sent as `Authorization: Bearer …`. |

`url` must be `http://` or `https://`, without a username or password in it (use `token`). A private or LAN address, or this machine, is fine; a link-local one (such as `169.254.169.254`), multicast and `0.0.0.0` are refused.

`events` switches each kind on or off. Every channel gets every message that is on.

| Event | Default | When |
|---|---|---|
| `firmware` | on | A tuner has newer firmware. Once per release, kept across restarts in `<settings file name without .json>.notified.json`. It does nothing while `firmwareCheck` is off, so the page won't let you switch it on until the check is on (and the API refuses). |
| `tuner` | on | A tuner has been unreachable for two minutes, and when it comes back. A reboot or a brief drop sends nothing. |
| `viewer` | on | A viewer was turned away because they were at their limit or the tuners were taken. The same viewer and reason is reported at most every half hour. |
| `request` | on | Someone who isn't a viewer asked to use a tuner for the first time, such as a friend who has just added Yagi in their media server. It names them (their Tailscale login or device, or their address); they appear on the Viewers page to be allowed. |
| `watching` | **off** | A viewer was given a tuner: who, which channel when the request names one, and on which tuner. A viewer is reported at most once every half hour per tuner, so changing channels or a media server connecting again isn't a message each time. |
| `unknown` | **off** | An address that isn't a viewer tried to use a tuner. A tuner port that can be reached from the internet is scanned all day, so each address is reported once a day, and at most six an hour in all. |

A message is sent once and not retried; if a channel is down you'll find `notification not sent` in the log, with the reason and without the token. The settings are read for every message, so changes need no restart.

## How the limits work

Limits count **tuners, not particular tuners**. Your own TVs take whichever tuner is free, so Yagi can't pin a viewer to one.

For a viewer on a device (call the tuner's real number of tuners *N*):

- **What the viewer is told** (`TunerCount` in `discover.json`): the least of *N − ownerReserve*, the `sharedCap` (if set) and the viewer's `limit`, never below zero. A DVR plans recordings around it.
- **When a stream is refused:** a request for a channel gets a **503** if any of these is true:
  - the viewer is already using their `limit` (`viewer-limit`);
  - the viewers together are already using the `sharedCap` (`shared-cap`);
  - the tuners in use by all viewers, plus this one, plus the ones your own devices are using (read from the tuner's own status), would leave fewer than `ownerReserve` free (`owner-reserve`);
  - the tuner is offline (`tuner-offline`).
- A request from an address that isn't a viewer gets a **403** (`not-allowed`).

The reason is in the `X-Yagi-Reason` response header, and each refusal is noted (at most once a minute per viewer, device and reason) in the Live page's timeline. A stream already running is never cut off to make room.

Example, with a 4-tuner device, `ownerReserve: 1` and one of *your* TVs watching: viewers can have at most 4 − 1 − 1 = 2 tuners at that moment. The Live page shows the tuner being held for you as **Reserved**.

## Command-line flags

| Flag | Meaning |
|---|---|
| `-config PATH` | The settings file, `yagi.json` in the current directory by default (`/config/yagi.json` in the Docker image). If it doesn't exist it is created with the defaults at start-up. If the default one can't be created, Yagi runs without saving; a `-config` you name that can't be created is an error. |
| `-allow-lan` | Keep the LAN ports open even if *Only on the tailnet* is on: the way back in if it locked you out. Also set by `YAGI_ALLOW_LAN`. See [Built-in Tailscale](tailscale.md#only-on-the-tailnet). |
| `-tuner HOST` | A tuner's address, to run one device without a settings file. Also read from the `YAGI_TUNER` environment variable. It is used only while the settings file has no `devices`. |
| `-tuner-id ID` | **Deprecated.** Add the tuner on the admin page, which learns its DeviceID. |
| `-listen ADDR` | **Deprecated.** Where to listen (default `:5004`). Add the tuner on the admin page, or set `listen` in the settings file. |
| `-admin-listen ADDR` | Where the admin pages are (default `:9244`, or `127.0.0.1:9244`). Also read from the `YAGI_ADMIN_LISTEN` environment variable, and from `adminListen` in the settings file. See [The admin port](#the-admin-port). |
| `-stream-port PORT` | **Deprecated.** The tuner's streaming port (default `5004`). Set `streamPort` in the settings file. |
| `-advertise URL` | **Deprecated.** The base URL handed to clients (default: the host they connected with). Set `advertise` in the settings file. |
| `-report-id ID` | **Deprecated.** Report this DeviceID to clients instead of the tuner's own. Set `reportId` in the settings file. |
| `-poll DURATION` | How often to check the tuners (default `5s`). |
| `-firmware-url URL` | Read the firmware changelog from here instead of SiliconDust's page (for a mirror, or testing). |
| `-version` | Print the version and exit. |

`-tuner`, `-tuner-id`, `-listen`, `-stream-port`, `-advertise` and `-report-id` describe a device, so they can't be combined with `devices` in the settings file. The last five are **deprecated**: they still work, Yagi logs a warning for each one you give, and they will be removed in a future release. Add the tuner on the admin page, or put those settings in the file. `-tuner` (and `YAGI_TUNER`) stay, as the way to start with a tuner and no settings to write.

### The admin port

The admin pages (the web page, its API, sign-in and the first-run setup link) are always on a port of their own, **`9244`** unless you say otherwise. Tuners have ports of their own too (`5004`, `5005`, …) and **a tuner is never on the admin port**. That way you can open the tuner ports to your viewers and keep the admin port to your own network or Tailscale.

To change it, the first of these that is set wins:

1. `-admin-listen :9244` on the command line (a space or `=` between the flag and its value);
2. `YAGI_ADMIN_LISTEN=:9244` in the environment, which is easiest in Docker;
3. `"adminListen": ":9244"` in the settings file.

A change takes effect when Yagi restarts. The admin address is `http://<this machine>:9244/`.

- Until you add a tuner, only the admin port is open. A tuner's port exists only for a tuner, so a port test of `5004` fails on a fresh install.
- A tuner can't be given the admin port, and the admin port can't be a port a tuner already uses: Yagi says so and stops, or refuses the change.
- `-listen` says where a tuner given on the command line goes (default `5004`).

`yagi discover [-wait 3s] [host …]` lists the HDHomeRun tuners it can find (a broadcast on your network, and a scan of the nearby subnet), one per line: DeviceID, address, number of tuners.
Add host addresses to look at those instead.

## Files Yagi keeps

- **The settings file** (the one you pass to `-config`): everything above.
- **`<settings file name without .json>.sessions.json`**, beside it: who is signed in with Plex, so a restart doesn't sign you out. Only a hash of each session is stored, never the session itself or your Plex token. It is created only if Plex sign-in is used, and only when there is a settings file.

- **`<settings file name without .json>.history.jsonl`**, beside it: who watched what, one line per session (which viewer or device, which tuner, which channel and show, when it began and ended, about how much data it was, and the average signal quality), kept for 30 days, for the Activity page. Without a settings file it is kept in memory and starts over with Yagi. A viewer's address is not stored; for your own devices, their address on your network is.
- **`<settings file name without .json>.events.jsonl`**, beside it: what happened to each tuner (it went offline, came back, moved to a new address, turned a stream away, had no channels), the same lines the tuner's page shows under Events, kept for 30 days and at most the newest 50 for each tuner. It is why those lines are still there after Yagi restarts or is updated, which is when you most want to see them. It stops growing at about 1 MB between restarts. Without a settings file the events are kept in memory and start over with Yagi.
- **`<settings file name without .json>.notified.json`**, beside it: which firmware release each tuner was last messaged about, so a restart doesn't send it again. It is only written when notifications are used and there is a settings file.

Back them up with your other configuration. There is no database, and nothing else is kept. Deleting the history file clears the Activity page, and deleting the events file clears the tuners' events. Logs go to standard error (`docker logs yagi`).

## What it checks on its own

| What | How often | Notes |
|---|---|---|
| The tuners | every 5 seconds (`-poll`) | After 3 failures in a row a tuner is shown offline, and Yagi starts searching for it by DeviceID (at most every 30 seconds), using a broadcast and a scan of the nearby subnet. |
| Firmware changelog | every 12 hours | Only when `firmwareCheck` is on. |
| Firmware file | never on a schedule | Downloaded only when an admin presses Install on a tuner's Firmware tab, and sent to that tuner over its control port (TCP 65001). Needs `firmwareCheck` on. |
| Program guide | every 6 hours | Only when `guide` is on. About 3.5 MB for a FLEX 4K. |

Notifications aren't on a schedule of their own: they are sent when the firmware check, the tuner check or a viewer's request notices something.

A restart begins again at the address in the settings file, and finds the tuner by its DeviceID. Yagi doesn't write a new address back into the file. Update `tuner` yourself, and reserve the address in your router, to skip the search.
