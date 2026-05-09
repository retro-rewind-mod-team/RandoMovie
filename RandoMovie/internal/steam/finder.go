package steam

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	GameFolder = "RetroRewind"
	VHSRelPath = "RetroRewind/Content/Movies/VHS"

	// RetroRewind.exe is the small launcher (~430 KB).
	// Shipping.exe is the actual game binary (~137 MB).
	// Anything under 2 MB is plausible for the launcher.
	MaxLauncherSize = 2 * 1024 * 1024 // 2 MB
	MinLauncherSize = 100 * 1024      // 100 KB — sanity floor for a real binary
)

var Categories = []string{
	"Action", "Adult", "Drama", "Fantasy", "Horror",
	"Kid", "Police", "Public", "Romance", "Scifi",
}

// FindCategoryVideo returns the path of the active .mp4 in the category folder.
// Non-.mp4 files (e.g. .bak) are ignored so backups left alongside the originals
// are never picked up as the active video.
func FindCategoryVideo(gamePath, category string) (string, error) {
	catDir := filepath.Join(VHSPath(gamePath), category)
	entries, err := os.ReadDir(catDir)
	if err != nil {
		return "", fmt.Errorf("cannot read category dir %s: %w", category, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".mp4") {
			return filepath.Join(catDir, name), nil
		}
	}

	return "", fmt.Errorf("no .mp4 found in %s", catDir)
}

func VHSPath(gamePath string) string {
	return filepath.Join(gamePath, VHSRelPath)
}

// ValidateGamePath checks that gamePath is actually the RetroRewind installation:
//  1. RetroRewind.exe must exist with a size that matches the launcher (~430 KB),
//     not the full game binary (~137 MB) that lives in a subdirectory.
//  2. At least 3 known VHS category directories must be present.
func ValidateGamePath(gamePath string) error {
	exePath := filepath.Join(gamePath, "RetroRewind.exe")
	info, err := os.Stat(exePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("RetroRewind.exe not found in %s", gamePath)
	}
	if err != nil {
		return fmt.Errorf("cannot read RetroRewind.exe: %w", err)
	}

	size := info.Size()
	if size > MaxLauncherSize {
		return fmt.Errorf(
			"RetroRewind.exe is %.1f MB — expected ~430 KB launcher, not the game binary",
			float64(size)/(1024*1024),
		)
	}
	if size < MinLauncherSize {
		return fmt.Errorf(
			"RetroRewind.exe is only %d bytes — file seems corrupted",
			size,
		)
	}

	// Require at least 3 known category directories so we don't accept an
	// arbitrary folder that happens to contain a small .exe.
	vhsPath := VHSPath(gamePath)
	found := 0
	for _, cat := range Categories {
		catDir := filepath.Join(vhsPath, cat)
		if st, err := os.Stat(catDir); err == nil && st.IsDir() {
			found++
		}
	}
	if found < 3 {
		return fmt.Errorf(
			"VHS directory structure invalid — only %d/%d categories found in %s",
			found, len(Categories), vhsPath,
		)
	}

	return nil
}

// FindRetroRewind tries auto-detection across all Steam libraries on the
// current OS. Returns an error if the game cannot be located.
func FindRetroRewind() (string, error) {
	for _, steamPath := range findSteamPaths() {
		for _, lib := range parseLibraryFolders(steamPath) {
			gamePath := filepath.Join(lib, "steamapps", "common", GameFolder)
			if err := ValidateGamePath(gamePath); err == nil {
				return gamePath, nil
			}
		}
	}
	return "", fmt.Errorf("RetroRewind not found — manual selection required")
}

// findSteamPaths returns the default Steam installation directories for the
// current OS. These are checked before parsing libraryfolders.vdf.
func findSteamPaths() []string {
	home, _ := os.UserHomeDir()

	switch runtime.GOOS {
	case "linux":
		return []string{
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
			"/var/games/Steam",
		}
	case "windows":
		return []string{
			`C:\Program Files (x86)\Steam`,
			`C:\Program Files\Steam`,
		}
	case "darwin":
		return []string{
			filepath.Join(home, "Library", "Application Support", "Steam"),
		}
	}
	return nil
}

// parseLibraryFolders parses Steam's libraryfolders.vdf to discover additional
// library paths. steamPath itself is always included as the first entry.
func parseLibraryFolders(steamPath string) []string {
	paths := []string{steamPath}

	candidates := []string{
		filepath.Join(steamPath, "steamapps", "libraryfolders.vdf"),
		filepath.Join(steamPath, "config", "libraryfolders.vdf"),
	}

	for _, vdfPath := range candidates {
		data, err := os.ReadFile(vdfPath)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, `"path"`) {
				parts := strings.SplitN(line, `"`, 5)
				if len(parts) >= 4 {
					paths = append(paths, parts[3])
				}
			}
		}
		break // stop at the first readable VDF file
	}

	return paths
}
