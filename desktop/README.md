# BuildBee desktop

> **Parked.** The desktop shell is out of scope and out of CI while BuildBee is
> rebuilt as a LAN web app. It needed cross-origin access to a Server, which the
> Server no longer offers; open the Server URL in a browser instead.


A **Tauri 2** shell that loads the existing `web/` UI and talks to a **Server**.
This is a v1 window + settings MVP (not a Gravity-style multi-window agent bench).

The Server (and Postgres, if you use it) **must run separately**. This app does
not bundle a database or start `buildbee-server` for you.

## What you get

- App name **BuildBee**, window title **BuildBee**, amber-B icon
- Dev: `tauri dev` starts Vite (`web/` `npm run dev`) at `http://127.0.0.1:5173` (`vite.config.ts` binds `127.0.0.1` so Linux does not sit on `[::1]` only)
- Prod: the bundle embeds `web/dist`
- Settings (`#/settings`): **Server** URL, default `http://127.0.0.1:8080`, persisted in local storage and the desktop config directory
- Hash routes still work (`#/invite/…`, `#/projects/…`, `#/projects/…?beside=…` for a two-pane split)
- Native menu: **Reload**, **Open Server URL**, **Quit**

## Prerequisites

| Tool | Notes |
| --- | --- |
| **Node.js** 20+ (22 matches CI) | For `web/` and `@tauri-apps/cli` |
| **Rust** 1.88+ (CI uses latest stable) | `rustup` — [https://rustup.rs](https://rustup.rs) |
| **Server** | `make run` or Compose. See the repo root README |

### Platform system libraries

**Linux (Debian/Ubuntu):**

```bash
sudo apt-get install -y \
  libwebkit2gtk-4.1-dev libgtk-3-dev libayatana-appindicator3-dev \
  librsvg2-dev patchelf libssl-dev
```

**macOS:** Xcode Command Line Tools (`xcode-select --install`). No extra WebKit package.

**Windows:** [Microsoft C++ Build Tools](https://visualstudio.microsoft.com/visual-cpp-build-tools/)
and WebView2 (current Windows 10/11 usually have it).

## Run (dev)

In one terminal, start a Server (memory store is enough):

```bash
make run
# or: cd server && go run ./cmd/server
# health: http://127.0.0.1:8080/healthz
```

In another:

```bash
cd desktop
npm install
npm run dev          # tauri dev → Vite + native window
```

Open **Settings** in the window if the Server is not on `http://127.0.0.1:8080`.
Invite links that use hash routes (`#/invite/<token>`) navigate in-app.

## Build (local installers)

```bash
cd desktop
npm install
npm run build        # runs web production build, then tauri build
```

Artifacts land under `desktop/src-tauri/target/release/bundle/`
(`.dmg` / `.app` on macOS, `.deb`/AppImage on Linux, `.msi`/`.exe` on Windows).

CI does **not** produce signed installers. See [Platform limits](#platform-limits).

## Cargo check (what CI runs)

```bash
cd desktop/src-tauri
cargo check
cargo test
```

This compiles the Rust shell without bundling a `.dmg` or signing anything.
On Linux you still need the WebKit/GTK dev packages above.

## Platform limits

- **CI** (Ubuntu): `cargo check` + `cargo test` only. No `.dmg`, `.msi`, or notarized artifacts.
- **Signing / notarization**: not configured. Local `npm run build` on macOS/Windows
  produces unsigned packages suitable for your own machine.
- **OAuth in the webview**: GitHub cookies from a remote Server are best-effort.
  Dev auth (`GITHUB_CLIENT_ID` unset) is the supported v1 path.
- **No Postgres / Server sidecar** in the desktop bundle.

## Layout

```
desktop/
  package.json          # @tauri-apps/cli
  scripts/gen-icons.py  # regenerate PNG / ICO / ICNS
  src-tauri/            # Rust host, menu, settings.json
  README.md
```

`tauri.conf.json` points `frontendDist` at `../../web/dist` and `devUrl` at the Vite port.
The web package stays independent — no Tauri npm dependency is added to `web/`.
