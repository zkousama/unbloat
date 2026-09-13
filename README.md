# unbloat

Get disk space back from WSL and Docker Desktop on Windows.

WSL and Docker keep everything in virtual disk files that grow and don't shrink. Deleting a 12 GB build cache inside one leaves C: exactly where it was until the disk file is compacted, and compacting returns little unless the disk was first trimmed from inside the distro that owns it. unbloat runs those steps in order and shows what each one costs before anything happens.

## What it cleans

- npm, npx and pip caches inside each running distro, and the pnpm store through `pnpm store prune`, which only removes packages no project references
- Docker's build cache, and images no container uses (unticked by default, since they're downloaded again when needed)
- files in `%TEMP%` older than 7 days, and the npm, npx, pnpm and yarn caches on Windows
- the virtual disks: each distro's `ext4.vhdx` and Docker's `docker_data.vhdx`

Docker volumes are never offered, named or anonymous. The official Postgres image keeps its data in an anonymous volume unless you give it a name.

## Install

Download `unbloat-amd64.exe` or `unbloat-arm64.exe` from the [latest release](https://github.com/zkousama/unbloat/releases/latest) and compare its hash with `SHA256SUMS`:

```powershell
Get-FileHash .\unbloat-amd64.exe -Algorithm SHA256
```

The executables aren't signed, so SmartScreen warns the first time you run one.

With Go installed you can build it instead:

```powershell
go install github.com/zkousama/unbloat/cmd/unbloat@latest
```

## Running it

Start it from PowerShell or Windows Terminal. Compacting shuts WSL down, which would end unbloat too if it ran inside a WSL shell, so it refuses to shut anything down when it finds it was started from one.

1. If it isn't running as administrator, it offers to start again as one. Compacting needs it; everything else works without.
2. It scans. The scan changes nothing, and a stopped distro isn't started just to be measured.
3. You tick what to clean. There are 2 totals: space freed inside the virtual disks, which reaches C: only when that disk is compacted, and space freed on C: by the run.
4. It lists the steps in the order they'll run. Press `c` to see the exact commands.
5. If a disk is being compacted, it lists what will stop, and refuses on battery below 50%.
6. It runs, then prints free space on C: before and after, and where the log is.

Every command and its output is logged to `%LOCALAPPDATA%\unbloat\logs`.

## Compacting

For each disk you tick:

1. `fstrim` runs as root inside the distro that owns the disk, and a stopped distro is started for it. Docker's disk is trimmed from inside `docker-desktop`.
2. `docker desktop stop`, then `wsl --shutdown`. If Docker Desktop doesn't stop, WSL is left running and nothing is compacted.
3. The disk file is checked for anything still holding it open.
4. `Optimize-VHD -Mode Full` where Windows has it, and `diskpart` with the disk attached read-only on Home editions.

A disk whose trim failed isn't compacted. Ctrl+C stops the run before its next step. A step that's already running, a compaction included, finishes first.

Disks are never switched to sparse mode. Current WSL releases refuse `--set-sparse` because of a data corruption risk.

## Checks

CI runs `go vet` and the tests on Linux and Windows, and builds both executables. The parsers are tested against output recorded on a real machine.

## License

MIT
