package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"minfo/internal/httpapi/transport"
	"minfo/internal/torrent"
)

type torrentRequest struct {
	InputPath string
	Cleanup   func()
	Options   torrent.Options
}

func parseTorrentFormRequest(r *http.Request) (torrentRequest, error) {
	inputPath, cleanup, err := transport.InputPath(r)
	if err != nil {
		return torrentRequest{}, err
	}

	options, err := parseTorrentOptions(r)
	if err != nil {
		cleanup()
		return torrentRequest{}, err
	}

	return torrentRequest{
		InputPath: inputPath,
		Cleanup:   cleanup,
		Options:   options,
	}, nil
}

func parseTorrentOptions(r *http.Request) (torrent.Options, error) {
	pieceLength, err := parseTorrentPieceLength(r.FormValue("piece_length"))
	if err != nil {
		return torrent.Options{}, err
	}
	options := torrent.Options{
		Format:      strings.TrimSpace(r.FormValue("format")),
		PieceLength: pieceLength,
		Private:     parseTorrentBool(r.FormValue("private")),
		Trackers:    splitTorrentFormList(r, "tracker_url"),
		WebSeeds:    splitTorrentFormList(r, "web_seed_url"),
		Comment:     strings.TrimSpace(r.FormValue("comment")),
		Source:      strings.TrimSpace(r.FormValue("source")),
	}
	if _, err := torrent.BuildMkbrrArgs("input", "output.torrent", options); err != nil {
		return torrent.Options{}, err
	}
	return options, nil
}

func parseTorrentPieceLength(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return torrent.DefaultPieceLength, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	if _, err := torrent.PieceLengthExponent(value); err != nil {
		return 0, err
	}
	return value, nil
}

func parseTorrentBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func splitTorrentFormList(r *http.Request, key string) []string {
	values := r.Form[key]
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, item := range strings.FieldsFunc(value, func(ch rune) bool {
			return ch == '\r' || ch == '\n'
		}) {
			item = strings.TrimSpace(item)
			if item != "" {
				result = append(result, item)
			}
		}
	}
	return result
}
