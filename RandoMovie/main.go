package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"RandoMovie/internal/backup"
	"RandoMovie/internal/config"
	"RandoMovie/internal/steam"
	"RandoMovie/internal/swap"
)

// ─── App State ───────────────────────────────────────────────

type RandoApp struct {
	fyneApp fyne.App
	window  fyne.Window
	cfg     *config.Config
	backup  *backup.State
	swapper *swap.Swapper
	busy    bool

	selectedCat     string
	selectedPoolIdx int

	// Widgets (need refresh)
	pathLabel     *widget.Label
	catList       *widget.List
	detailTitle   *widget.Label
	statusLabel   *widget.Label
	origHashLabel *widget.Label
	currHashLabel *widget.Label
	swapBtn       *widget.Button
	restoreBtn    *widget.Button
	poolList      *widget.List
	statusBar     *widget.Label

	// Action buttons — disabled while a background operation is running
	detectBtn     *widget.Button
	browseBtn     *widget.Button
	enterPathBtn  *widget.Button
	randomBtn     *widget.Button
	restoreAllBtn *widget.Button
	verifyBtn     *widget.Button
}

// ─── Main ────────────────────────────────────────────────────

func main() {
	ra := &RandoApp{selectedPoolIdx: -1}
	ra.fyneApp = app.New()
	ra.window = ra.fyneApp.NewWindow("🎬 RandoMovie")
	ra.window.Resize(fyne.NewSize(850, 600))
	ra.cfg = config.Load()

	ra.buildUI()
	ra.initGamePath()

	ra.window.ShowAndRun()
}

// ─── UI Build ────────────────────────────────────────────────

func (ra *RandoApp) buildUI() {

	// ── Top: Path bar ──
	ra.pathLabel = widget.NewLabel("Searching…")
	ra.detectBtn = widget.NewButton("Auto-Detect", func() { ra.autoDetect() })
	ra.browseBtn = widget.NewButton("Browse…", func() { ra.browsePath() })
	ra.enterPathBtn = widget.NewButton("Enter Path…", func() { ra.enterPath() })
	pathBar := container.NewHBox(
		widget.NewLabelWithStyle("Game:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		ra.pathLabel,
		layout.NewSpacer(),
		ra.detectBtn,
		ra.browseBtn,
		ra.enterPathBtn,
	)

	// ── Left: Category list ──
	ra.catList = widget.NewList(
		func() int { return len(steam.Categories) },
		func() fyne.CanvasObject {
			return widget.NewLabel("placeholder text")
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			cat := steam.Categories[id]
			prefix := "⬜"
			if ra.backup != nil {
				if info, ok := ra.backup.Categories[cat]; ok {
					if info.IsOriginal {
						prefix = "✅"
					} else {
						prefix = "🔶"
					}
				}
			}
			o.(*widget.Label).SetText(prefix + " " + cat)
		},
	)
	ra.catList.OnSelected = func(id widget.ListItemID) {
		ra.selectedCat = steam.Categories[id]
		ra.updateDetail()
	}

	leftPanel := container.NewBorder(
		widget.NewLabelWithStyle("Categories", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		ra.catList,
	)

	// ── Right top: Detail panel ──
	ra.detailTitle = widget.NewLabelWithStyle(
		"Select a category", fyne.TextAlignLeading, fyne.TextStyle{Bold: true},
	)
	ra.statusLabel = widget.NewLabel("")
	ra.origHashLabel = widget.NewLabel("")
	ra.currHashLabel = widget.NewLabel("")

	ra.swapBtn = widget.NewButton("📁 Swap Video…", func() { ra.swapSelected() })
	ra.swapBtn.Disable()
	ra.restoreBtn = widget.NewButton("↩ Restore", func() { ra.restoreSelected() })
	ra.restoreBtn.Disable()

	detailPanel := container.NewVBox(
		ra.detailTitle,
		widget.NewSeparator(),
		ra.statusLabel,
		ra.origHashLabel,
		ra.currHashLabel,
		layout.NewSpacer(),
		container.NewHBox(ra.swapBtn, ra.restoreBtn),
	)

	// ── Right bottom: Video pool ──
	ra.poolList = widget.NewList(
		func() int { return len(ra.cfg.VideoPool) },
		func() fyne.CanvasObject {
			return widget.NewLabel("video.mp4")
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(filepath.Base(ra.cfg.VideoPool[id]))
		},
	)
	ra.poolList.OnSelected = func(id widget.ListItemID) {
		ra.selectedPoolIdx = id
	}

	poolHeader := container.NewHBox(
		widget.NewLabelWithStyle("Video Pool (for Random)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		widget.NewButton("+ Add", func() { ra.addVideo() }),
		widget.NewButton("+ Enter path…", func() { ra.enterVideoPath() }),
		widget.NewButton("− Remove", func() { ra.removeVideo() }),
	)

	poolPanel := container.NewBorder(poolHeader, nil, nil, nil, ra.poolList)

	// ── Right: Detail + Pool ──
	rightSide := container.NewVSplit(detailPanel, poolPanel)
	rightSide.SetOffset(0.55)

	// ── Center: Categories | Detail+Pool ──
	mainSplit := container.NewHSplit(leftPanel, rightSide)
	mainSplit.SetOffset(0.25)

	// ── Bottom: Action bar ──
	ra.statusBar = widget.NewLabel("Ready")
	ra.randomBtn = widget.NewButton("🎲 Random All", func() { ra.randomAll() })
	ra.restoreAllBtn = widget.NewButton("↩ Restore All", func() { ra.restoreAll() })
	ra.verifyBtn = widget.NewButton("✓ Verify", func() { ra.verifyBackups() })
	actionBar := container.NewHBox(
		ra.randomBtn,
		ra.restoreAllBtn,
		ra.verifyBtn,
		layout.NewSpacer(),
		ra.statusBar,
	)

	// ── Assemble ──
	ra.window.SetContent(
		container.NewBorder(pathBar, actionBar, nil, nil, mainSplit),
	)
}

// ─── Busy state ──────────────────────────────────────────────

// setBusy disables all action buttons while a background operation runs
// (prevents concurrent file I/O on large files) and re-enables them after.
func (ra *RandoApp) setBusy(b bool) {
	ra.busy = b
	for _, btn := range []*widget.Button{
		ra.detectBtn, ra.browseBtn, ra.enterPathBtn,
		ra.randomBtn, ra.restoreAllBtn, ra.verifyBtn,
		ra.swapBtn, ra.restoreBtn,
	} {
		if b {
			btn.Disable()
		} else {
			btn.Enable()
		}
	}
	if !b {
		ra.updateDetail() // re-syncs swapBtn/restoreBtn based on actual state
	}
}

// ─── Initialization ──────────────────────────────────────────

func (ra *RandoApp) initGamePath() {
	if ra.cfg.GamePath != "" {
		if err := steam.ValidateGamePath(ra.cfg.GamePath); err == nil {
			ra.setGamePath(ra.cfg.GamePath)
			return
		}
	}
	ra.autoDetect()
}

func (ra *RandoApp) autoDetect() {
	if ra.busy {
		return
	}
	path, err := steam.FindRetroRewind()
	if err != nil {
		ra.pathLabel.SetText("Not found — use Browse or Enter Path")
		ra.statusBar.SetText("Auto-detection failed")
		dialog.ShowInformation("Not Found",
			"RetroRewind could not be found automatically.\n"+
				"Please select the game folder via Browse or Enter Path.",
			ra.window,
		)
		return
	}
	ra.setGamePath(path)
}

func (ra *RandoApp) browsePath() {
	if ra.busy {
		return
	}
	d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil || uri == nil {
			return
		}
		path := uri.Path()
		if err := steam.ValidateGamePath(path); err != nil {
			dialog.ShowError(
				fmt.Errorf("invalid game directory:\n%s", err),
				ra.window,
			)
			return
		}
		ra.setGamePath(path)
	}, ra.window)
	d.Show()
}

// enterPath opens a text-entry dialog so the user can type or paste a game
// path that the file browser cannot reach, such as UNC network shares.
func (ra *RandoApp) enterPath() {
	if ra.busy {
		return
	}
	entry := widget.NewEntry()
	entry.SetPlaceHolder(`e.g. Z:\Games\RetroRewind  or  \\NAS\Games\RetroRewind`)
	if ra.cfg.GamePath != "" {
		entry.SetText(ra.cfg.GamePath)
	}
	d := dialog.NewCustomConfirm("Enter Game Path", "Set", "Cancel", entry, func(ok bool) {
		if !ok {
			return
		}
		path := strings.TrimSpace(entry.Text)
		if path == "" {
			return
		}
		if err := steam.ValidateGamePath(path); err != nil {
			dialog.ShowError(fmt.Errorf("invalid game directory:\n%s", err), ra.window)
			return
		}
		ra.setGamePath(path)
	}, ra.window)
	d.Show()
}

// enterVideoPath lets the user type or paste a video file path for the pool,
// useful when the file lives on a network share not reachable via the file browser.
func (ra *RandoApp) enterVideoPath() {
	entry := widget.NewEntry()
	entry.SetPlaceHolder(`e.g. Z:\Videos\myfilm.mp4  or  \\NAS\Videos\myfilm.mp4`)
	d := dialog.NewCustomConfirm("Enter Video Path", "Add", "Cancel", entry, func(ok bool) {
		if !ok {
			return
		}
		path := strings.TrimSpace(entry.Text)
		if path == "" {
			return
		}
		ra.cfg.AddVideo(path)
		ra.cfg.Save()
		ra.poolList.Refresh()
		ra.statusBar.SetText(fmt.Sprintf("Added %s to pool", filepath.Base(path)))
	}, ra.window)
	d.Show()
}

func (ra *RandoApp) setGamePath(path string) {
	ra.cfg.GamePath = path
	ra.cfg.Save()

	display := path
	if len(display) > 50 {
		display = "…" + display[len(display)-47:]
	}
	ra.pathLabel.SetText(display)

	ra.backup = backup.NewState(path)
	ra.swapper = swap.New(path, ra.backup)

	ra.statusBar.SetText("Creating backups…")
	ra.setBusy(true)

	go func() {
		count, errs := ra.backup.BackupAll()
		if len(errs) > 0 {
			ra.statusBar.SetText(fmt.Sprintf("Backed up %d (%d errors)", count, len(errs)))
		} else {
			ra.statusBar.SetText(fmt.Sprintf("Ready — %d categories backed up", count))
		}
		ra.setBusy(false)
		ra.catList.Refresh()
	}()
}

// ─── Detail Panel ────────────────────────────────────────────

func (ra *RandoApp) updateDetail() {
	if ra.selectedCat == "" || ra.backup == nil {
		ra.detailTitle.SetText("Select a category")
		ra.statusLabel.SetText("")
		ra.origHashLabel.SetText("")
		ra.currHashLabel.SetText("")
		ra.swapBtn.Disable()
		ra.restoreBtn.Disable()
		return
	}

	ra.detailTitle.SetText("📂 " + ra.selectedCat)
	if !ra.busy {
		ra.swapBtn.Enable()
	}

	info, exists := ra.backup.Categories[ra.selectedCat]
	if !exists {
		ra.statusLabel.SetText("Status: No backup yet")
		ra.origHashLabel.SetText("")
		ra.currHashLabel.SetText("")
		ra.restoreBtn.Disable()
		return
	}

	if info.IsOriginal {
		ra.statusLabel.SetText("Status: Original ✅")
		ra.restoreBtn.Disable()
	} else {
		ra.statusLabel.SetText("Status: Modified 🔶")
		if !ra.busy {
			ra.restoreBtn.Enable()
		}
	}

	ra.origHashLabel.SetText("Original: " + info.OriginalHash[:16] + "…")
	ra.currHashLabel.SetText("Current:  " + info.CurrentHash[:16] + "…")
}

// ─── Category Actions ────────────────────────────────────────

func (ra *RandoApp) swapSelected() {
	if ra.selectedCat == "" || ra.swapper == nil || ra.busy {
		return
	}
	cat := ra.selectedCat

	d := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil || reader == nil {
			return
		}
		videoPath := reader.URI().Path()
		reader.Close()

		ra.statusBar.SetText(fmt.Sprintf("Swapping %s…", cat))
		ra.setBusy(true)
		go func() {
			swapErr := ra.swapper.SwapVideo(cat, videoPath)
			if swapErr != nil {
				dialog.ShowError(swapErr, ra.window)
			} else {
				ra.statusBar.SetText(fmt.Sprintf("Swapped %s ← %s", cat, filepath.Base(videoPath)))
				ra.catList.Refresh()
			}
			ra.setBusy(false)
		}()
	}, ra.window)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".mp4"}))
	d.Show()
}

func (ra *RandoApp) restoreSelected() {
	if ra.selectedCat == "" || ra.backup == nil || ra.busy {
		return
	}
	cat := ra.selectedCat
	ra.statusBar.SetText(fmt.Sprintf("Restoring %s…", cat))
	ra.setBusy(true)
	go func() {
		restoreErr := ra.backup.Restore(cat)
		if restoreErr != nil {
			dialog.ShowError(restoreErr, ra.window)
		} else {
			ra.statusBar.SetText(fmt.Sprintf("Restored %s ✅", cat))
			ra.catList.Refresh()
		}
		ra.setBusy(false)
	}()
}

// ─── Global Actions ──────────────────────────────────────────

func (ra *RandoApp) randomAll() {
	if ra.swapper == nil || ra.busy {
		return
	}
	if len(ra.cfg.VideoPool) == 0 {
		dialog.ShowInformation("Empty Pool",
			"Add videos to the pool first\nbefore using Random.",
			ra.window,
		)
		return
	}

	ra.statusBar.SetText("Randomising…")
	ra.setBusy(true)
	go func() {
		assignments, randErr := ra.swapper.RandomSwap(steam.Categories, ra.cfg.VideoPool)
		if randErr != nil {
			dialog.ShowError(randErr, ra.window)
		} else {
			ra.statusBar.SetText(fmt.Sprintf("🎲 Randomly assigned %d videos", len(assignments)))
			ra.catList.Refresh()
		}
		ra.setBusy(false)
	}()
}

func (ra *RandoApp) restoreAll() {
	if ra.backup == nil || ra.busy {
		return
	}
	dialog.ShowConfirm("Restore All",
		"Restore all categories to their original videos?",
		func(ok bool) {
			if !ok {
				return
			}
			ra.statusBar.SetText("Restoring all…")
			ra.setBusy(true)
			go func() {
				count, errs := ra.backup.RestoreAll()
				if len(errs) > 0 {
					ra.statusBar.SetText(fmt.Sprintf("Restored %d (%d errors)", count, len(errs)))
				} else {
					ra.statusBar.SetText(fmt.Sprintf("Restored %d categories ✅", count))
				}
				ra.setBusy(false)
				ra.catList.Refresh()
			}()
		}, ra.window,
	)
}

func (ra *RandoApp) verifyBackups() {
	if ra.backup == nil || ra.busy {
		return
	}
	ra.statusBar.SetText("Verifying…")
	ra.setBusy(true)
	go func() {
		results := ra.backup.VerifyBackups()
		ok, bad := 0, 0
		for _, valid := range results {
			if valid {
				ok++
			} else {
				bad++
			}
		}
		if bad == 0 {
			dialog.ShowInformation("Verification",
				fmt.Sprintf("All %d backups intact ✅", ok),
				ra.window,
			)
		} else {
			dialog.ShowError(
				fmt.Errorf("%d OK, %d corrupted ⚠️", ok, bad),
				ra.window,
			)
		}
		ra.statusBar.SetText(fmt.Sprintf("Verified: %d OK, %d bad", ok, bad))
		ra.setBusy(false)
	}()
}

// ─── Video Pool ──────────────────────────────────────────────

func (ra *RandoApp) addVideo() {
	d := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil || reader == nil {
			return
		}
		path := reader.URI().Path()
		reader.Close()

		ra.cfg.AddVideo(path)
		ra.cfg.Save()
		ra.poolList.Refresh()
		ra.statusBar.SetText(fmt.Sprintf("Added %s to pool", filepath.Base(path)))
	}, ra.window)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".mp4"}))
	d.Show()
}

func (ra *RandoApp) removeVideo() {
	idx := ra.selectedPoolIdx
	if idx < 0 || idx >= len(ra.cfg.VideoPool) {
		return
	}
	name := filepath.Base(ra.cfg.VideoPool[idx])
	ra.cfg.RemoveVideo(ra.cfg.VideoPool[idx])
	ra.cfg.Save()
	ra.selectedPoolIdx = -1
	ra.poolList.UnselectAll()
	ra.poolList.Refresh()
	ra.statusBar.SetText(fmt.Sprintf("Removed %s from pool", name))
}
