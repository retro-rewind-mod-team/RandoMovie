package swap

import (
	"fmt"
	"math/rand"
	"path/filepath"

	"RandoMovie/internal/backup"
	"RandoMovie/internal/steam"
	"RandoMovie/internal/util"
)

type Swapper struct {
	GamePath string
	Backup   *backup.State
}

func New(gamePath string, bs *backup.State) *Swapper {
	return &Swapper{GamePath: gamePath, Backup: bs}
}

// SwapVideo replaces the active video for category with the file at videoPath.
// It ensures a backup of the original exists before overwriting anything.
func (s *Swapper) SwapVideo(category, videoPath string) error {
	if err := s.Backup.BackupOriginal(category); err != nil {
		return fmt.Errorf("backup before swap failed: %w", err)
	}

	// Resolve the actual destination path inside the game folder,
	// e.g. .../VHS/Action/RR_Channel_Adult.mp4
	dstFile, err := steam.FindCategoryVideo(s.GamePath, category)
	if err != nil {
		return fmt.Errorf("cannot find target file for %s: %w", category, err)
	}

	oldHash := s.Backup.Categories[category].CurrentHash

	if err := util.CopyFile(videoPath, dstFile); err != nil {
		return fmt.Errorf("swap failed for %s: %w", category, err)
	}

	newHash, err := util.HashFile(dstFile)
	if err != nil {
		return fmt.Errorf("hash after swap failed: %w", err)
	}

	s.Backup.RecordSwap(category, oldHash, newHash, videoPath)
	return s.Backup.Save()
}

// RandomSwap assigns a randomly chosen video from videoPool to each category.
// When there are more categories than pool entries the pool is reused round-robin
// (after shuffling), so every category always receives a video.
func (s *Swapper) RandomSwap(categories []string, videoPool []string) (map[string]string, error) {
	if len(videoPool) == 0 {
		return nil, fmt.Errorf("video pool is empty")
	}
	if len(categories) == 0 {
		return nil, fmt.Errorf("no categories selected")
	}

	shuffled := make([]string, len(videoPool))
	copy(shuffled, videoPool)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	assignments := make(map[string]string)

	for i, cat := range categories {
		video := shuffled[i%len(shuffled)]
		if err := s.SwapVideo(cat, video); err != nil {
			return assignments, fmt.Errorf("random swap failed at %s: %w", cat, err)
		}
		assignments[cat] = filepath.Base(video)
	}
	return assignments, nil
}
