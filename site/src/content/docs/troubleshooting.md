---
title: "Troubleshooting"
---

:::tip[Start with the log]
It usually says what is wrong. In Docker: `docker logs yagi`. Every refusal says why, with the address it came from, and streams opening and closing are logged too. The requests a media server makes over and over (`discover.json`, `lineup.json`) are only logged at the *Debug* level, so they don't bury the rest: set the level under *Settings → Logs* when you need them.

A request that never shows up in the log (at *Debug* for an ordinary one) never arrived, which points at the network or a firewall, not at Yagi.
:::

- [A port test fails, or the page won't open](#a-port-test-fails-or-the-page-wont-open)
- [The log says "refused a connection from a public internet address"](#refused-a-connection-from-a-public-internet-address)
- [A page or the tuner address just hangs, then times out](#it-just-hangs-then-times-out)
- ["This address can't open the admin page"](#this-address-cant-open-the-admin-page)
- [The tuner address shows "this address is not allowed to use this tuner"](#the-bare-address-says-this-address-is-not-allowed-to-use-this-tuner)
- [The tuner shows as offline](#the-tuner-shows-as-offline)
- [A viewer's media server can't add the tuner, or a channel won't play](#a-viewers-media-server-cant-add-the-tuner-or-a-channel-wont-play)
- [A 503 when watching](#a-503-when-watching)
- [Plex shows the tuner twice, or the wrong channels](#plex-shows-the-tuner-twice-or-with-the-wrong-channels)
- [Settings I change don't stick](#settings-i-change-dont-stick)
- [Yagi won't start](#yagi-wont-start)
- [I'm signed out after a restart, or the page says "Reconnecting…"](#im-signed-out-after-a-restart-or-the-page-says-reconnecting)
- [No channel logos or "what's on"](#no-channel-logos-or-whats-on)
- [Install problems](#install-problems)
- [Docker problems](#docker-problems)
- [Asking for help](#asking-for-help)

## A port test fails, or the page won't open

Check the log first: when Yagi starts it prints the address of the admin page (`admin page open=…`), on `9244` unless you changed it. Then:

- **Which port?** The admin page is on `9244`. A tuner's port (`5004`, …) opens only after you add that tuner, so a test of `5004` fails on a fresh install. That is normal.
- **Is it running?** `docker ps` or `ps aux | grep yagi`. If it stopped, its last log lines say why (see [Yagi won't start](#yagi-wont-start)).
- **Is it listening?** On the machine: `curl -i http://localhost:9244/`. If that works but another computer's browser can't connect, it is the firewall (below).
- **Docker without `network_mode: host`** publishes nothing unless you add `-p 9244:9244 -p 5004:5004`. Use host networking.

## "refused a connection from a public internet address"

Yagi serves private networks only, so a connection from a public internet address is dropped before it gets any answer, and the other end just sees it hang. The log line names the address and the port. Usually it means someone is reaching you over the open internet (a forwarded port): put them on Tailscale or a VPN, and list the address Yagi sees from there. If it is your own network, check the address in the log: a LAN that has public IPv6 addresses on a *different* network than the machine's counts as public until the machine is on that network too. See [Networking](networking.md#not-over-the-open-internet).

A viewer who lists a public address can never connect, and the Viewers page says so on their row.

## It just hangs, then times out

Nothing came back at all, not even a refusal. The connection is being dropped before it reaches Yagi.

1. **The machine's firewall** is the usual cause. Allow the viewer's address to reach the tuner's port, and yours to reach the admin port (`9244`). See [Networking](networking.md#the-machines-firewall).
2. **The wrong address, or the wrong network.** Are both machines on the same Tailscale, VPN or LAN? `tailscale ping <address>` works at the network level even when a port is blocked, so use `curl -i http://<address>:5004/discover.json` to test the port itself.
3. **Tailscale access controls** may not allow that port between those two machines.
4. **Safari or another browser changed the address**, for example to `https://`. Yagi speaks plain `http://`. Type it in full, with `:5004`.

Check the log, with its level set to *Debug* so that ordinary requests show: if the request appears there, it arrived and this section isn't your problem.

## "This address can't open the admin page"

The page loaded (it is public so you can sign in), but Yagi doesn't consider your address an admin. Once you have a viewer, only these can open the admin pages:

- this machine itself (`http://localhost:9244/`, or through `ssh -L 9244:127.0.0.1:9244 yourserver`);
- addresses listed under `admin` in the settings;
- a Plex account you have added, if *Sign in with Plex* is on (the page then shows a **Sign in with Plex** button).

To fix it, add your address to `admin` (the log line for your request shows the address Yagi saw), or turn on Plex sign-in from an address that is already an admin. If you have locked yourself out completely, edit the settings file on the machine, add your address to `admin`, and send the process `SIGHUP` (or restart it).

If it stopped working after it had worked, your computer's address probably changed (a new DHCP lease). Reserve its address in your router so it stays the same.

Your address might not be the one you expect. Behind Docker's bridge network, a reverse proxy, or after NAT, Yagi sees the gateway's address. See [Networking](networking.md#if-yagi-runs-in-docker).

## The bare address says "this address is not allowed to use this tuner"

The address itself is fine, but whatever opened it (a script, a media server, `curl`) isn't one of your viewers, so it is refused. A browser at the admin port, `http://host:9244/`, gets the web page, where you can sign in. (A browser at a tuner's port, `5004`, is refused: that port is only for media servers.) If a browser shows this message, it is on an older version (before 2026.10.13 the page was at `/yagi/`), or the address is not on your admin list: see above.

If you open Yagi by a name like `mini.local`, your phone or laptop may connect over IPv6 using a link-local address (`fe80::…`). Yagi now treats those as ordinary addresses, so you can list `fe80::/10` under `admin` if you want the whole local network, but it is usually easier to use the machine's IPv4 address.

## The tuner shows as offline

Yagi checks the tuner every 5 seconds. After 3 failures in a row it shows it offline, and then searches for it by its DeviceID (a broadcast, and a scan of the nearby subnet) every 30 seconds.

- **Is the tuner on?** Power, the Ethernet cable (an extender or switch can fail), and the router. Can you reach `http://<tuner address>/discover.json` from the Yagi machine?
- **Is it listed at all?** The *Tuners* page lists the tuners being shared . Press *Add tuner* to search the network again or add one by its address. In Docker, searching needs host networking.
- **Did its address change?** If you gave the tuner's `id` in the settings (or Yagi has reached it before), it finds the new address by itself, usually within a minute, and the Live page's timeline records "Tuner moved … (found by DeviceID …)". If you didn't, run `yagi discover` and put the address and `id` in the settings.
- **A restart starts at the address in the settings file.** When Yagi finds the tuner somewhere else it saves the new address there (the log says *saved the tuner's new address*), so the next start looks in the right place. A tuner it hasn't reached since starting is searched for straight away, which can still take up to half a minute. Reserve its address in your router to avoid moving at all.
- **Restarting or updating is not an outage.** While Yagi restarts a tuner or installs firmware on it, the page says *Restarting…* or *Updating firmware…*, and no offline message is sent. If it stays that way for more than a few minutes, check the tuner's power and cable: the result on the tuner's *Firmware* or *Device* tab says what Yagi saw.
- **A broadcast can't cross networks.** If the tuner is on a different subnet or VLAN from Yagi, the search can't find it. Give its address in `tuner`.
- **In Docker**, the search needs host networking.

## A viewer's media server can't add the tuner, or a channel won't play

From the **viewer's server** (not their TV), try `curl -i http://<address>:5004/discover.json`:

| You get | It means |
|---|---|
| Nothing, then a timeout | The connection never arrived. A firewall, or the wrong address. See [it just hangs](#it-just-hangs-then-times-out). |
| `403` and `X-Yagi-Reason: not-allowed` | It arrived, but the sender isn't a viewer. They are listed on the Viewers page under *Asking to use your tuner*: press **Allow**. Or add the address the log shows under *Viewers*; after Tailscale or a VPN it is the viewer's address on that network. |
| `502 tuner unreachable` | It got through, but the tuner is offline. See [the tuner shows as offline](#the-tuner-shows-as-offline). |
| `200` with Yagi's address in `BaseURL` | Yagi is fine. The problem is in the media server. |

Also: a viewer with a **limit of 0** is paused and every stream is refused. And **the server itself** must reach the address; a TV or phone being able to reach it is no help.

## A 503 when watching

A `503` is Yagi deliberately saying "no tuner for you right now", the same answer a busy tuner gives. The `X-Yagi-Reason` header says why (`curl -i`, or look at the Live page's timeline):

| Reason | What happened | What to change |
|---|---|---|
| `viewer-limit` | This viewer is already using all their tuners. | Raise their limit under *Viewers*, or stop a stream. |
| `shared-cap` | All viewers together are using the shared cap. | Raise *Max tuners for viewers* in *Settings → Access → Limits*. |
| `owner-reserve` | The tuners left are being held for your own devices. | Lower *Tuners reserved for you*. Remember the reserve is **in addition to** what your own TVs are using right now. |
| `tuner-offline` | The tuner is offline. | See above. |

## A media server says the channel isn't available, or the guide is empty

Plex says "channel not available" when it asked for a stream and didn't get video, and shows an empty guide when it didn't get the list of channels. It doesn't say why. When Yagi can see the reason, it says so at the top of every page, and records it on the Live page's timeline. A notice goes away by itself after fifteen minutes without the trouble happening again (an empty channel list stays until the tuner has its channels again), and the ✕ hides it for as long as the same trouble goes on. It comes back if it happens again later.

| What the page says | What happened | What to do |
|---|---|---|
| **The tuner isn't answering at …** | Yagi can't reach the tuner. | See [the tuner shows as offline](#the-tuner-shows-as-offline). |
| **The tuner turned a stream away: 503 …** | The tuner itself refused, which is what a busy tuner does: every one of its tuners was in use, by a viewer, a recording or something outside Yagi. Anything in brackets (*it said: …*) is the tuner's own reason. | Look at the Live page to see what is using the tuners. |
| **… was refused a tuner: …** | Yagi turned a viewer away by its own rules: their limit, the shared cap, or the tuners held for you. | See [a 503 when watching](#a-503-when-watching). |
| **The tuner answered a media server's request for its channels with an empty list** | The tuner had no channels to give. That can happen during a rescan, or just after the tuner restarts, and it clears by itself when it has them again. | Wait a minute or two. If it stays, open the tuner's own page and check that it has scanned for channels. |
| Nothing | Yagi saw no trouble. The problem is on the media server's side, or in reaching Yagi. | Check the media server's own log, and try `curl -i http://<address>:5004/lineup.json` from the media server. |

To look at the tuner's side yourself, open the tuner's page, then its **Device** tab, and press **Check now**. It asks the tuner a few questions, one at a time, and says what it finds: whether the tuner answers, whether it is the tuner Yagi follows, whether it has channels, whether a tuner is free (and who is using the others), and whether the tuner's own error counters are above zero, which means a weak signal or a network that loses data. It only reads: it never tunes a channel or changes anything. "What the tuner prints about itself" shows the tuner's raw debug text, which SiliconDust support may ask for.

Yagi only sees what happens between the media server and the tuner. It can't see Plex's own guide data or its settings.

## Plex shows the tuner twice, or with the wrong channels

Media servers treat devices with the **same DeviceID as the same device**. If the viewer's Plex already has this tuner from an older setup (a relay, or by its own address), adding it again by Yagi's address can merge with, or fight, the old entry.

- Remove the old DVR/tuner in the viewer's Plex first, then add `<address>:5004`.
- Or give Yagi's copy its own ID with `reportId` (8 hex digits) in the settings, which makes it a separate device. Restart afterwards, since it is a device setting.
- Live TV in Plex needs Plex Pass, and the guide data comes from Plex, not from Yagi.

## Settings I change don't stick

- **"Start Yagi with -config to edit settings"**: it could not create its settings file where it was started (a read-only directory, usually), so there is nowhere to save. The log says so at start-up. Start it with `-config /path/to/yagi.json` somewhere writable.
- **Changes to `devices` don't apply**: the list of devices only changes on restart. The log says "restart Yagi to apply that". Everything else reloads (`SIGHUP`, or any change made in the web page).
- **Hand edits are ignored**: tell it to reload (`SIGHUP`), or restart. A mistake in the file keeps the old settings and logs the error ("settings reload failed, keeping the current ones").
- **Saving fails with a permissions error**: Yagi needs to be able to create files in the settings file's folder (it saves by writing a temporary file and renaming it). In Docker, make `./config` owned by the user in `user:`.

## Yagi won't start

The message says why. The common ones:

- **"flag provided but not defined"**, or a message about putting a space between a flag and its value: write `-admin-listen :9244` or `-admin-listen=:9244`, not `-admin-listen:9244`.
- **"address already in use"**: something else has that port. The message says which of Yagi's ports it was; change it (`-admin-listen`, `YAGI_ADMIN_LISTEN` or `adminListen` for the admin page, `listen` in the settings file for a tuner). On a Mac, port `5000` belongs to AirPlay, which is why Yagi doesn't use it.

- **An unknown key**: the settings file is strict, so a typo is an error: `json: unknown field "viewrs"`.
- **A bad value**: a device name with a space, a `listen` without a port, an `id` that isn't 8 hex digits, a viewer with no addresses, the same address listed for two viewers, two devices on one port.
- **"no tuner is set up yet"** is not an error: Yagi is running with only the web page. Open it and add a tuner from the list it finds, or by address.
- **"… can't be combined with devices declared in the settings file"**: you passed `-tuner`/`-listen`/… and also have `devices` in the file. Choose one.

## I'm signed out after a restart, or the page says "Reconnecting…"

- Sign-ins are kept in a file beside the settings file, `<settings>.sessions.json`, so a restart shouldn't sign you out. If it does, Yagi couldn't create its settings file, or it couldn't write the sessions file (check the log), or the sign-in had expired (12 hours idle, 7 days at most).
- A page left open while Yagi restarts shows **Reconnecting…**. It opens a new connection by itself, so it recovers on its own. If the server no longer recognizes you, it shows the sign-in screen.

## No channel logos or "what's on"

- It is on by default: check *Settings → Privacy → Show what's on*.
- It needs the machine to reach `api.hdhomerun.com` and `img.hdhomerun.com`, and the tuner has to have reported its guide key. The log shows `guide fetch failed` with the reason.
- It only appears for channels being watched right now.
- The picture behind a tile comes from the same guide, through Yagi, so it needs the same two addresses. A tile without a picture is normal when the guide has none for that program.

## Installing firmware or restarting says it didn't work

- **"Didn't take" or "still running …"**: the tuner accepted the file and came back on the old version. Yagi never tries a second time by itself. Check what the tuner's own page says, and try again from the tuner's *Firmware* tab when nothing is using it.
- **"SiliconDust has no firmware file …"**: Yagi builds the download address from the tuner's firmware family and the release the check found, and SiliconDust doesn't document those addresses. The firmware check keeps working; open an issue with the model and version.
- **"The tuner has not come back"**: give it a few minutes, then check power and the Ethernet cable and unplug it for ten seconds. Nothing more is sent to it.
- **Restart says it kept answering**: the tuner didn't appear to restart. Try again, or restart it from its own page or by unplugging it.
- Neither runs while something is using the tuner unless you confirm that you want to interrupt it.

## Testing a channel's signal says it can't

- The test needs a tuner nothing is using, and never interrupts a stream. With all of the tuner's tuners in use the button is greyed out; try again when one is free.
- It can't run while the tuner is scanning, restarting or being updated, or while another test is running.
- *No signal* means the tuner tuned to the channel but couldn't lock on to it within a few seconds: the antenna doesn't get it, or it is too weak. It is a real answer, not an error.
- If the tuner won't tune to a channel at all, the message says what it replied.

## The Activity page is empty or missing

- It is switched on by default under *Settings → Privacy → Keep activity history*, and the *Activity* item is hidden while it is off.
- It records what is in use as it happens, so it starts empty, and a session shorter than 15 seconds is not kept.
- Without a settings file it is kept in memory and starts over with Yagi. Otherwise it is in `<settings file>.history.jsonl` beside it.

## Install problems

- **`yagi: command not found` after the install script**: it installed into `~/.local/bin`, which isn't on your `PATH`. The script printed the line to add (`export PATH="$HOME/.local/bin:$PATH"`) to `~/.profile`, `~/.zshrc` or similar; open a new terminal after.
- **The script says the download doesn't match its checksum, or the signature isn't valid**: nothing was installed, on purpose. Run it again in case the download was cut short; if it says the same, do not use that download, and [report it](../SECURITY.md).
- **The script says there is no download for this system**: Yagi is built for Linux and macOS, on `amd64` and `arm64`. On anything else, use Docker or build it yourself.
- **`brew services start yagi` runs but the page doesn't open**: read the log, `$(brew --prefix)/var/log/yagi.log`, which has the setup link and says why if it couldn't start (a port already in use, for one). On Linux, `brew services` needs systemd.
- **Where are the settings?** Homebrew: `$(brew --prefix)/var/yagi/yagi.json`. The script with `--service`: `/var/lib/yagi/yagi.json`. The script without it, or a build: `yagi.json` in the folder you started it from, unless you pass `-config`.
- **After updating, the page still shows the old version**: a running service keeps running the old program until it restarts: `brew services restart yagi`, or `sudo systemctl restart yagi`.

## Docker problems

- **It sees every request as coming from `172.x.x.x`, can't find the tuner, or adding a viewer offers no Tailscale address**: use `network_mode: host`.
- **Can't write its settings**: the `./config` folder isn't owned by the user in `user:`.
- **A build fails with a DNS error** (`dl-cdn.alpinelinux.org`): the default Docker network on some firewalld machines can't reach the internet during a build. Use the published image instead of building, or build with `docker build --network host`.
- **The firewall's rules aren't visible from inside the container**, so the Add viewer steps don't detect it. That is expected.

## Built-in Tailscale

- **The status row says *Connecting…* or *Not signed in yet*.** Open the *Sign in to Tailscale* link on *Settings → Access → Tailscale*, and approve the device if your tailnet asks. Set `TS_AUTHKEY` for an unattended start.
- **The HTTPS link doesn't answer.** The first visit waits while the certificate is issued (Yagi asks for it as soon as HTTPS is on, so this is usually over before you click). If the row shows a reason instead of a link, your tailnet probably has HTTPS certificates turned off: Tailscale admin console → DNS → *Enable HTTPS*. Also check that the device you browse from is on the tailnet.
- **"This address can't open the admin page" over the tailnet.** Nobody on the tailnet is an admin yet: add `tailscale:you@example.com` (or `100.64.0.0/10`) under *Admin access → Admin addresses*, from the machine itself.
- **Locked out after turning on *Only on the tailnet*.** From the machine itself, `http://127.0.0.1:<port>/` still works and the switch can be turned off there. In Docker the host's browser doesn't arrive as loopback, so start the container with `YAGI_ALLOW_LAN=1` (or `-allow-lan`), turn the switch off, and remove it.

## Asking for help

[Open an issue](https://github.com/johnschult/yagi/issues) and include:

- the **version** (the bottom of the web page, or `yagi -version`) and how you run it (Docker or not, and the network setup);
- what you did and what you expected;
- the relevant lines of the **log**, with any addresses you'd rather not share replaced;
- your settings file, with addresses changed if you prefer. It has no passwords or tokens in it.

:::caution[Keep these private]
Please don't post your tuner's `DeviceAuth` or a Plex token anywhere.
:::
