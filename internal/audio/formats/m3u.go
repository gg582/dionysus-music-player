package formats

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func ParseM3U(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []string
	scanner := bufio.NewScanner(f)
	baseDir := filepath.Dir(path)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !filepath.IsAbs(line) {
			line = filepath.Join(baseDir, line)
		}
		entries = append(entries, line)
	}
	return entries, scanner.Err()
}
