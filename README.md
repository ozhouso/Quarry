# Quarry

Command line tool for pulling the files out of DayZ's PBO archives. It unpacks compressed files and turns binarized configs (`config.bin`, `.rvmat`) back into text you can read.

I made it because the existing extractors were slow and hadn't been updated in a long time. It reads every PBO in parallel from one shared worker pool, so a few huge archives don't hold everything up, and it remembers what it extracted so re-runs only touch what changed.

On my machine a full extraction of the game (about 135,000 files, 30 GB) takes around 3 minutes. Most of that is disk speed. Scripts and configs only takes about a second.

It's a single Go executable with no dependencies. Windows, Linux and macOS builds are on the releases page.

## Usage

```
quarry [destination] [options]
```

Destination defaults to `~/dayz-extract`. The game is found through Steam (stable or experimental).

| Option | |
|---|---|
| `-x`, `--experimental` | use the Experimental install |
| `-g`, `--game PATH` | game folder, skips Steam detection |
| `-i`, `--include LIST` | only extract files matching these globs |
| `-e`, `--exclude LIST` | skip files matching these globs |
| `--dlc LIST` | only use these DLC folders, or `none` |
| `-m`, `--mod LIST` | also extract mods: `@Name`, a folder, or a single `.pbo` |
| `--config FILE` | use this `quarry.json` instead of looking for one |
| `-j`, `--jobs N` | number of workers (default 2 x CPU cores) |
| `--raw` | leave binarized configs as they are |
| `--force` | extract everything again, ignore what was saved last time |
| `--list` | print the PBOs it would use and exit |
| `-q`, `--quiet` | only print errors |
| `--no-color` | no colours (`NO_COLOR` works too) |
| `-v`, `--version` | print the version |
| `-h`, `--help` | help |

Lists are comma separated. Globs only match the file name, not the whole path.

```bash
quarry                                    # everything, default folder
quarry P:\                                # everything, to P:\
quarry out -i "*.c,*.cpp,*.bin"           # scripts and configs only
quarry out -e "*.paa,*.rvmat"             # no textures or materials
quarry out -x                             # experimental branch
quarry out -m "@CF,@Community-Online-Tools"
quarry out -m "C:\mods\@MyMod,C:\somewhere\thing.pbo"
```

If you double click the exe it asks where to extract to and waits for a keypress at the end so the window doesn't vanish.

## Other commands

| | |
|---|---|
| `quarry ls [pbo]` | list the PBOs, or the files inside one |
| `quarry cat <file>` | print a file straight out of the PBOs |
| `quarry grep <text>` | search scripts, configs and layouts without extracting anything |
| `quarry diff [dest]` | show what the last extraction added, changed or removed |
| `quarry dlc` | show the DLC folders it found |
| `quarry init` | write a starter `quarry.json` |
| `quarry update` | install the newest release |

All of them take `-h`, and the game options (`-g`, `-x`, `-m`, `--dlc`) work on all of them except `update` and `diff`.

```bash
quarry ls scripts -i "*PlayerBase*"
quarry cat PlayerBase.c                   # matches on the end of the path
quarry cat DZ/AI/config.cpp               # binarized configs come out as text
quarry grep OnRPC -i "*.c" -C 2
quarry grep "class\s+PlayerBase" -r       # -r for regex
quarry diff                               # after a game update, what did it touch
```

`grep` is case insensitive unless you pass `-s`. By default it only looks at text-ish files; use `-i` to search other types.

## DLC and quarry.json

Any folder in the game directory with an `addons` folder full of PBOs counts as DLC (Nasdara, Sakhal and so on). They're all included by default. `quarry dlc` lists what it found.

To pick, either use `--dlc nasdara,sakhal` (`--dlc none` for base game only) or save it in a `quarry.json`:

```json
{
  "dlc": ["nasdara", "sakhal"],
  "mods": ["@CF"],
  "destination": "P:\\"
}
```

`quarry init` creates one. All keys are optional. Leave `dlc` out and every installed DLC is used, `[]` means none. It looks in the current folder first, then next to the exe. Command line options win over the file.

## What ends up on disk

Each PBO goes into a folder named after its prefix, the way the game sees it:

```
out/
  DZ/
    AI/config.cpp
    characters/...
  scripts/
    1_Core/...
    3_Game/...
  .quarry.json            what was extracted, so re-runs can skip unchanged PBOs
  .quarry-changes.json    history that `quarry diff` reads
```

Files that already match what's on disk aren't rewritten, so if you keep the output in git you only see real changes after an update. If a PBO changed, files it no longer contains are deleted. Changing `--include`, `--exclude` or `--raw` between runs makes it extract everything again, so you don't end up with a mix. Both dot files are safe to delete.

## Updating

```
quarry update
quarry update --check
```

Downloads the latest release from GitHub, checks it against the checksums published with the release, and replaces the exe. It also checks once a day in the background after a normal run and prints a line if there's something newer. Set `QUARRY_NO_UPDATE_CHECK=1` to turn that off.

## Building

Needs Go 1.22 or newer.

```bash
git clone https://github.com/ozhouso/Quarry
cd Quarry
go build -o quarry ./cmd/quarry
```

For a Windows exe from Linux or WSL:

```bash
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o quarry.exe ./cmd/quarry
```

In PowerShell the environment variable goes on its own line: `$env:GOOS="windows"`, then the build.

Releases are built by the workflow in `.github/workflows/release.yml` when a `v*` tag is pushed.

## Things to know

- The exe isn't signed, so Windows SmartScreen may complain the first time. More info, then Run anyway, or build it yourself.
- It works under WSL. `C:\...` paths are translated to `/mnt/c/...`.
- The coloured progress bar needs a UTF-8 terminal. Windows Terminal and most Linux terminals are fine, otherwise it falls back to plain ASCII.
- It only reads from your DayZ install, it never writes to it.
- Compressed files are checked against the checksum stored in the PBO. If one fails it's reported and that PBO is retried on the next run.

## License

MIT
