package audio

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

// ParsePLS reads a PLS playlist and returns the list of file/URL locations.
func ParsePLS(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []string
	scanner := bufio.NewScanner(f)
	inPlaylist := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "[playlist]" {
			inPlaylist = true
			continue
		}
		if !inPlaylist {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "file") {
			// File1=..., File2=...
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				entries = append(entries, strings.TrimSpace(parts[1]))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// ParseXSPF reads an XSPF playlist and returns the list of locations.
func ParseXSPF(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var xspf struct {
		Tracks []struct {
			Location string `xml:"location"`
		} `xml:"trackList>track"`
	}
	// Use a simple XML decoder; XSPF namespace can be ignored for basic parsing.
	decoder := xml.NewDecoder(f)
	if err := decoder.Decode(&xspf); err != nil {
		return nil, fmt.Errorf("xspf decode: %w", err)
	}

	var entries []string
	for _, t := range xspf.Tracks {
		if strings.TrimSpace(t.Location) != "" {
			entries = append(entries, strings.TrimSpace(t.Location))
		}
	}
	return entries, nil
}
