package audio

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/jfreymuth/oggvorbis"
	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/meta"
)

// ExtractReplayGain reads the ReplayGain track gain (in dB) from supported
// file tags.  If no tag is present, 0 is returned.
func ExtractReplayGain(path string) (float64, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3":
		return extractReplayGainMP3(path)
	case ".flac":
		return extractReplayGainFLAC(path)
	case ".ogg":
		return extractReplayGainOgg(path)
	}
	return 0, nil
}

func extractReplayGainMP3(path string) (float64, error) {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return 0, err
	}
	defer tag.Close()

	frames := tag.GetFrames(tag.CommonID("User defined text information frame"))
	for _, f := range frames {
		if txxx, ok := f.(id3v2.UserDefinedTextFrame); ok {
			if strings.EqualFold(txxx.Description, "REPLAYGAIN_TRACK_GAIN") {
				return parseGain(txxx.Value)
			}
		}
	}
	return 0, nil
}

func extractReplayGainFLAC(path string) (float64, error) {
	stream, err := flac.Open(path)
	if err != nil {
		return 0, err
	}
	defer stream.Close()

	for _, block := range stream.Blocks {
		if vc, ok := block.Body.(*meta.VorbisComment); ok {
			for _, tag := range vc.Tags {
				if len(tag) == 2 && strings.EqualFold(tag[0], "REPLAYGAIN_TRACK_GAIN") {
					return parseGain(tag[1])
				}
			}
		}
	}
	return 0, nil
}

func extractReplayGainOgg(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	ch, err := oggvorbis.GetCommentHeader(f)
	if err != nil {
		return 0, err
	}
	for _, c := range ch.Comments {
		key, val, ok := strings.Cut(c, "=")
		if ok && strings.EqualFold(key, "REPLAYGAIN_TRACK_GAIN") {
			return parseGain(val)
		}
	}
	return 0, nil
}

func parseGain(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "dB")
	s = strings.TrimSuffix(s, " dB")
	s = strings.TrimSpace(s)
	return strconv.ParseFloat(s, 64)
}
