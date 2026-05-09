package util

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// HashFile returns the SHA-256 hex digest of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CopyFile copies src to dst, overwriting dst if it already exists.
// If the copy fails mid-way, the incomplete dst is removed so callers
// never see a partial file. The close error is returned explicitly to
// catch OS-level flush failures (e.g. disk full on NFS/BTRFS).
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst) // don't leave a partial file behind
		return err
	}
	return out.Close() // surfaces flush errors that io.Copy alone won't catch
}
