# aaapk

CLI package manager for Android over ADB. Works with F-Droid repos and Google Play.

## Usage

```
aaapk install <query>    # search, pick, install
aaapk update             # check managed packages for updates
aaapk refresh            # re-fetch repo indexes
aaapk repo list          # show configured repos
aaapk repo add <name>    # add fdroid or gplay repo
aaapk repo rm <name>
aaapk repo enable <name>
aaapk repo disable <name>
```

Pass `--debug` before the command for verbose output.

## How it works

Searches all enabled repos in parallel. F-Droid repos are queried via their `index-v1.json`. Google Play is accessed through Aurora's token dispenser, doing a device checkin with your phone's actual properties read over ADB.

## Files it leaves

| Path | Where | What |
|---|---|---|
| `~/.config/aaapk/config.json` | Host | Repo list and settings |
| `~/.cache/aaapk/<repo>-index.json` | Host | Cached F-Droid indexes (refreshed every 24h) |
| `/sdcard/.aaapk.json` | Device | Ledger of installed packages, versions, and sources (used for update checks) |

## Requirements

`adb` in your PATH with a device connected.