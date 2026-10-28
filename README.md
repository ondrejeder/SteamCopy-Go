# SteamCopy Go

Copy or move installed Steam games between Windows and Linux libraries using an external drive. At startup the app shows registered local Steam libraries and looks for backup libraries on attached storage at its root or one folder below it. Use Reverse direction to show games on the selected external library and choose a local destination. The selected total is calculated from actual files; the preflight checks it again. You can add a local library or browse to external storage manually. The original appmanifest is copied with each game.

## Use

1. Steam can remain open. Pause downloads and updates for selected games while transferring; changes to game data or manifests can cause verification to fail.
2. Run the Windows executable, or run `chmod +x steamcopy-linux-arm64 && ./steamcopy-linux-arm64` on an ARM64 Linux device such as Odin 3 with Armada OS. The UI opens in your browser. Keep the application running while transferring.
3. Select the external storage and choose games from local Steam libraries. Use Reverse direction to copy back from the external library to a selected local library. Destination free space is displayed automatically. Choose Copy or Move, and check space before starting. Refresh after connecting or disconnecting a disk. For deeper or unusual locations, choose Browse, select a folder and confirm with Add or Use. USB detection is best effort; select or add paths manually if a library is grouped incorrectly.
   Games already present in the destination are marked and cannot be selected. Copying to external storage checks the selected external library; copying back checks all detected local libraries. A destination with only a manifest or only a game folder is also marked as a conflict. Detection does not compare file contents.
4. Copy verification compares relative paths, file counts and sizes and hashes each manifest. Enable SHA-256 to verify all game content. Move requires full hashes and two explicit confirmations before original data is removed.

The transfer screen shows the percentage and copied GiB out of the total, plus an estimated copying speed and remaining copy time based on recent progress. The estimate excludes verification and source deletion; it pauses while verification is running and adjusts as speed changes. The bar can reach 100% while verification is still running; wait for the completion message.

The destination is `steamapps/common/<game>` and `steamapps/appmanifest_<appid>.acf`. Steam needs to recognize the destination library; restart Steam after the operation and use Steam's verify files if necessary. Moving files does not guarantee game compatibility with ARM64, FEX, or Proton. Saved games, shader caches, compatibility tools, and Proton prefixes are not copied.

Transfer failures leave source data intact and may leave a `.steamcopy-*.partial` folder or a game folder without a manifest. Review these manually before retrying; the app does not automatically resume or delete them. It never intentionally overwrites existing target game folders or manifests. A failed source-removal step may leave a partially completed move; inspect the message before doing anything else.

The app rejects links and special files inside game folders. Linux source file permissions are copied when the filesystem supports them. An intermediate filesystem such as exFAT can lose Unix permissions, so native Linux titles may need Steam verification/reacquisition. FAT32 cannot hold files 4 GiB or larger. The source and destination must be mounted and writable, and 1 GiB of free space is reserved. Backup media format must be readable on both machines.

## Build

Go 1.25 or newer, no CGO or external modules:

On Windows, run `powershell -ExecutionPolicy Bypass -File .\build.ps1`. You can pass `-Go <path-to-go.exe>` when Go is not on PATH. Alternatively:

```powershell
$env:GOOS='windows'; $env:GOARCH='amd64'; go test ./...
$env:CGO_ENABLED='0'
$env:GOOS='windows'; $env:GOARCH='amd64'; go build -o dist/steamcopy-windows-amd64.exe .
$env:GOOS='linux'; $env:GOARCH='arm64'; go build -o dist/steamcopy-linux-arm64 .
```

The application opens a normal browser window, never kiosk or fullscreen mode. Closing its last window stops the application after a short delay. During a transfer the browser asks for confirmation; if you confirm, copying is cancelled safely before the app exits. If source removal is already underway, the app waits for it to finish. The browser controls the text and availability of the confirmation dialog. Abrupt browser or device termination may leave a partial copy.

For Linux desktop use, keep `Start-SteamCopy.sh` beside `steamcopy-linux-arm64`, make both executable with `chmod +x Start-SteamCopy.sh steamcopy-linux-arm64`, then open the script from the file manager using its Run action. This does not require a terminal window. If launched from a dedicated terminal, the app process ends on window close; an existing interactive shell remains open and is never forcibly closed.

The local server listens on a random loopback port and uses a session token for API requests.
