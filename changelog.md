## Bug Fixes & Improvements

### Async Operations for Large Files (>3 GB)
All blocking operations now run in goroutines to prevent UI freezes:
- BackupAll (when setting game path)
- SwapVideo / RestoreSelected
- RandomSwap / RestoreAll
- VerifyBackups

While an operation is running, `setBusy(true)` disables all action buttons (Auto-Detect, Browse, Enter Path, Random All, Restore All, Verify, Swap, Restore) to prevent concurrent operations. The status bar shows real-time progress ("Creating backups…", "Swapping…", etc.).

### Network Drive Support
Two new buttons for entering paths manually:
- **"Enter Path…"** in the top path bar: opens a text dialog to enter network paths like `\\NAS\Games\RetroRewind` or `Z:\Games\RetroRewind`
- **"+ Enter path…"** in the video pool: same functionality for adding video files from network drives
