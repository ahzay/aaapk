# aaapk

CLI package manager for Android over ADB. Works with F-Droid repos and Google Play.

## Why

Xiaomi CN ROMs don't ship with Google Play or Play Services. The built-in store forces you to sign in with a Xiaomi account just to sideload third-party APKs.

It can also be used for installing Google Play Services itself on these CN ROMs (tested on Redmi Note 13 Pro):
```bash
aaapk install com.google.android.gms
```

so you can bootstrap a usable phone from a clean flash without installing shady third party utilities (looking at you Xiaomi Google Installer).

Beyond Xiaomi devices, you shouldn't need to install Aurora Store just to get trusted APKs from the Play Store onto your device.

## Requirements

- `adb` in your PATH with a device connected and `adb` turned on (you will have to trust your computer)
- Linux or macOS (should work in WSL, untested)
- [mise-en-place](https://mise.jdx.dev)

## Install

```
git clone https://github.com/ahzay/aaapk
cd aaapk
mise trust
mise run install
```

This builds the binary and puts it in `~/.local/bin`. Make sure that's on your PATH:

```
export PATH="$HOME/.local/bin:$PATH"
```

## Usage

```
aaapk install <query>    # search, pick, install
aaapk list               # show managed apps
aaapk update             # check managed packages for updates
aaapk refresh            # re-fetch repo indexes
aaapk repo list          # show configured repos
```

Pass `--debug` before the command for verbose output.

## How it works

Searches all enabled repos in parallel. F-Droid repos are queried via their `index-v1.json`. Google Play is accessed through Aurora's token dispenser, doing a device checkin with your phone's actual properties read over ADB.

## Files

| Path | Where | What |
|---|---|---|
| `~/.config/aaapk/config.json` | Host | Repo list and settings |
| `~/.cache/aaapk/<repo>-index.json` | Host | Cached F-Droid indexes (refreshed every 24h) |
| `/sdcard/.aaapk.json` | Device | Ledger of installed packages, versions, and sources |

