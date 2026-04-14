package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/afkarxyz/SpotiFLAC/backend"
)

var version = "dev"

func main() {
	// Flags
	services := flag.String("service", "tidal", "Comma-separated services to try in order: tidal,deezer,qobuz,amazon")
	filenameFormat := flag.String("filename-format", "title-artist", "Filename format: title-artist, title, or custom with {title},{artist},{album},{year},{isrc}")
	audioFormat := flag.String("format", "LOSSLESS", "Audio format: LOSSLESS, MP3, AAC")
	embedLyrics := flag.Bool("lyrics", false, "Embed lyrics in the file")
	allowFallback := flag.Bool("fallback", true, "Allow fallback between services")
	showVersion := flag.Bool("version", false, "Show version")
	jsonOutput := flag.Bool("json", false, "Output result as JSON")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "SpotiFLAC CLI - Download Spotify tracks in FLAC\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  spotiflac-cli [options] <spotify-url> <output-dir>\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  spotiflac-cli https://open.spotify.com/track/xxx ./music\n")
		fmt.Fprintf(os.Stderr, "  spotiflac-cli -service tidal,deezer https://open.spotify.com/track/xxx ./music\n")
		fmt.Fprintf(os.Stderr, "  spotiflac-cli -filename-format \"{title}\" https://open.spotify.com/track/xxx ./music\n")
		fmt.Fprintf(os.Stderr, "  spotiflac-cli -json https://open.spotify.com/track/xxx ./music\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("spotiflac-cli %s\n", version)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	spotifyURL := args[0]
	outputDir := args[1]

	// Validate URL
	if !strings.Contains(spotifyURL, "open.spotify.com/") {
		fmt.Fprintf(os.Stderr, "Error: invalid Spotify URL\n")
		os.Exit(1)
	}

	// Create output dir if needed
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Parse services list
	serviceList := strings.Split(*services, ",")
	for i := range serviceList {
		serviceList[i] = strings.TrimSpace(serviceList[i])
	}

	// Determine what we're downloading (track, album, playlist)
	if strings.Contains(spotifyURL, "/track/") {
		err := downloadTrack(spotifyURL, outputDir, serviceList, *filenameFormat, *audioFormat, *embedLyrics, *allowFallback, *jsonOutput)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else if strings.Contains(spotifyURL, "/album/") || strings.Contains(spotifyURL, "/playlist/") {
		err := downloadCollection(spotifyURL, outputDir, serviceList, *filenameFormat, *audioFormat, *embedLyrics, *allowFallback, *jsonOutput)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Error: URL must be a Spotify track, album, or playlist\n")
		os.Exit(1)
	}
}

func extractSpotifyID(url string) string {
	// https://open.spotify.com/track/110ipbVsI5JspwxB0uIG7r?si=xxx → 110ipbVsI5JspwxB0uIG7r
	parts := strings.Split(url, "/")
	for i, p := range parts {
		if (p == "track" || p == "album" || p == "playlist") && i+1 < len(parts) {
			id := parts[i+1]
			if idx := strings.Index(id, "?"); idx != -1 {
				id = id[:idx]
			}
			return id
		}
	}
	return ""
}

type TrackInfo struct {
	SpotifyID   string
	Name        string
	Artist      string
	Album       string
	AlbumArtist string
	ReleaseDate string
	CoverURL    string
	ISRC        string
	Duration    int
	TrackNumber int
	DiscNumber  int
	TotalTracks int
	TotalDiscs  int
	Copyright   string
	Publisher   string
	Composer    string
}

func fetchMetadata(spotifyURL string) (*TrackInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data, err := backend.GetFilteredSpotifyData(ctx, spotifyURL, false, 0, ", ", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %v", err)
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to encode metadata: %v", err)
	}

	var resp struct {
		Track struct {
			Name        string `json:"name"`
			Artists     string `json:"artists"`
			AlbumName   string `json:"album_name"`
			AlbumArtist string `json:"album_artist"`
			ReleaseDate string `json:"release_date"`
			Images      string `json:"images"`
			ISRC        string `json:"isrc"`
			DurationMs  int    `json:"duration_ms"`
			TrackNumber int    `json:"track_number"`
			DiscNumber  int    `json:"disc_number"`
			TotalTracks int    `json:"total_tracks"`
			TotalDiscs  int    `json:"total_discs"`
			Copyright   string `json:"copyright"`
			Publisher   string `json:"publisher"`
			Composer    string `json:"composer"`
			SpotifyID   string `json:"spotify_id"`
			UPC         string `json:"upc"`
		} `json:"track"`
	}

	if err := json.Unmarshal(jsonData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %v", err)
	}

	spotifyID := resp.Track.SpotifyID
	if spotifyID == "" {
		spotifyID = extractSpotifyID(spotifyURL)
	}

	return &TrackInfo{
		SpotifyID:   spotifyID,
		Name:        resp.Track.Name,
		Artist:      resp.Track.Artists,
		Album:       resp.Track.AlbumName,
		AlbumArtist: resp.Track.AlbumArtist,
		ReleaseDate: resp.Track.ReleaseDate,
		CoverURL:    resp.Track.Images,
		ISRC:        resp.Track.ISRC,
		Duration:    resp.Track.DurationMs / 1000,
		TrackNumber: resp.Track.TrackNumber,
		DiscNumber:  resp.Track.DiscNumber,
		TotalTracks: resp.Track.TotalTracks,
		TotalDiscs:  resp.Track.TotalDiscs,
		Copyright:   resp.Track.Copyright,
		Publisher:    resp.Track.Publisher,
		Composer:     resp.Track.Composer,
	}, nil
}

func downloadTrack(spotifyURL, outputDir string, services []string, filenameFormat, audioFormat string, embedLyrics, allowFallback, jsonOutput bool) error {
	if !jsonOutput {
		fmt.Println("Fetching metadata...")
	}

	track, err := fetchMetadata(spotifyURL)
	if err != nil {
		return err
	}

	if !jsonOutput {
		fmt.Printf("Track: %s - %s\n", track.Name, track.Artist)
		fmt.Printf("Album: %s (%s)\n", track.Album, track.ReleaseDate)
	}

	// Try each service in order
	for _, service := range services {
		if !jsonOutput {
			fmt.Printf("Trying %s...\n", service)
		}

		result := doDownload(track, outputDir, service, filenameFormat, audioFormat, embedLyrics, allowFallback)

		if result.Success {
			if jsonOutput {
				out, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(out))
			} else {
				fmt.Printf("Downloaded: %s (via %s)\n", filepath.Base(result.File), service)
			}
			return nil
		}

		if !jsonOutput {
			fmt.Printf("  Failed (%s): %s\n", service, result.Error)
		}
	}

	errMsg := fmt.Sprintf("all services failed for: %s - %s", track.Name, track.Artist)
	if jsonOutput {
		out, _ := json.MarshalIndent(map[string]interface{}{
			"success": false,
			"error":   errMsg,
			"track":   track.Name,
			"artist":  track.Artist,
		}, "", "  ")
		fmt.Println(string(out))
	}
	return fmt.Errorf(errMsg)
}

func downloadCollection(spotifyURL, outputDir string, services []string, filenameFormat, audioFormat string, embedLyrics, allowFallback, jsonOutput bool) error {
	if !jsonOutput {
		fmt.Println("Fetching collection metadata...")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	data, err := backend.GetFilteredSpotifyData(ctx, spotifyURL, true, time.Second, ", ", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch collection: %v", err)
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to encode metadata: %v", err)
	}

	var collection struct {
		Tracks []struct {
			Name        string `json:"name"`
			Artist      string `json:"artist"`
			Album       string `json:"album"`
			AlbumArtist string `json:"album_artist"`
			ReleaseDate string `json:"release_date"`
			CoverURL    string `json:"cover_url"`
			ISRC        string `json:"isrc"`
			Duration    int    `json:"duration"`
			TrackNumber int    `json:"track_number"`
			DiscNumber  int    `json:"disc_number"`
			TotalTracks int    `json:"total_tracks"`
			TotalDiscs  int    `json:"total_discs"`
			SpotifyID   string `json:"spotify_id"`
		} `json:"tracks"`
	}

	if err := json.Unmarshal(jsonData, &collection); err != nil {
		return fmt.Errorf("failed to parse collection: %v", err)
	}

	total := len(collection.Tracks)
	if !jsonOutput {
		fmt.Printf("Found %d tracks\n\n", total)
	}

	var succeeded, failed int
	for i, t := range collection.Tracks {
		if !jsonOutput {
			fmt.Printf("[%d/%d] %s - %s\n", i+1, total, t.Name, t.Artist)
		}

		track := &TrackInfo{
			SpotifyID:   t.SpotifyID,
			Name:        t.Name,
			Artist:      t.Artist,
			Album:       t.Album,
			AlbumArtist: t.AlbumArtist,
			ReleaseDate: t.ReleaseDate,
			CoverURL:    t.CoverURL,
			ISRC:        t.ISRC,
			Duration:    t.Duration,
			TrackNumber: t.TrackNumber,
			DiscNumber:  t.DiscNumber,
			TotalTracks: t.TotalTracks,
			TotalDiscs:  t.TotalDiscs,
		}

		downloaded := false
		for _, service := range services {
			result := doDownload(track, outputDir, service, filenameFormat, audioFormat, embedLyrics, allowFallback)
			if result.Success {
				if !jsonOutput {
					fmt.Printf("  OK (%s)\n", service)
				}
				succeeded++
				downloaded = true
				break
			}
		}
		if !downloaded {
			if !jsonOutput {
				fmt.Printf("  FAILED\n")
			}
			failed++
		}
	}

	if !jsonOutput {
		fmt.Printf("\nDone: %d/%d succeeded, %d failed\n", succeeded, total, failed)
	}

	if jsonOutput {
		out, _ := json.MarshalIndent(map[string]interface{}{
			"total":     total,
			"succeeded": succeeded,
			"failed":    failed,
		}, "", "  ")
		fmt.Println(string(out))
	}

	return nil
}

type DownloadResult struct {
	Success bool   `json:"success"`
	File    string `json:"file,omitempty"`
	Error   string `json:"error,omitempty"`
	Service string `json:"service,omitempty"`
}

func doDownload(track *TrackInfo, outputDir, service, filenameFormat, audioFormat string, embedLyrics, allowFallback bool) DownloadResult {
	var filename string
	var err error

	switch service {
	case "tidal":
		downloader := backend.NewTidalDownloader("")
		filename, err = downloader.Download(
			track.SpotifyID, outputDir, audioFormat, filenameFormat,
			false, 0, track.Name, track.Artist, track.Album, track.AlbumArtist,
			track.ReleaseDate, false, track.CoverURL, false,
			track.TrackNumber, track.DiscNumber, track.TotalTracks, track.TotalDiscs,
			track.Copyright, track.Publisher, track.Composer, ", ", track.ISRC,
			fmt.Sprintf("https://open.spotify.com/track/%s", track.SpotifyID),
			allowFallback, false, false, false,
		)

	case "deezer":
		// Deezer uses ISRC-based lookup via the songlink/analysis pipeline
		downloader := backend.NewTidalDownloader("")
		filename, err = downloader.Download(
			track.SpotifyID, outputDir, audioFormat, filenameFormat,
			false, 0, track.Name, track.Artist, track.Album, track.AlbumArtist,
			track.ReleaseDate, false, track.CoverURL, false,
			track.TrackNumber, track.DiscNumber, track.TotalTracks, track.TotalDiscs,
			track.Copyright, track.Publisher, track.Composer, ", ", track.ISRC,
			fmt.Sprintf("https://open.spotify.com/track/%s", track.SpotifyID),
			allowFallback, false, false, false,
		)

	case "qobuz":
		downloader := backend.NewQobuzDownloader()
		isrc := track.ISRC
		if isrc == "" {
			client := backend.NewSongLinkClient()
			isrc, _ = client.GetISRCDirect(track.SpotifyID)
		}
		quality := audioFormat
		if quality == "" || quality == "LOSSLESS" {
			quality = "6"
		}
		filename, err = downloader.DownloadTrackWithISRC(
			isrc, outputDir, quality, filenameFormat,
			false, 0, track.Name, track.Artist, track.Album, track.AlbumArtist,
			track.ReleaseDate, false, track.CoverURL, false,
			track.TrackNumber, track.DiscNumber, track.TotalTracks, track.TotalDiscs,
			track.Copyright, track.Publisher, track.Composer, ", ",
			fmt.Sprintf("https://open.spotify.com/track/%s", track.SpotifyID),
			allowFallback, false, false, false,
		)

	case "amazon":
		downloader := backend.NewAmazonDownloader()
		filename, err = downloader.DownloadBySpotifyID(
			track.SpotifyID, outputDir, audioFormat, filenameFormat,
			"", "", false, 0, track.Name, track.Artist, track.Album, track.AlbumArtist,
			track.ReleaseDate, track.CoverURL, track.TrackNumber, track.DiscNumber,
			track.TotalTracks, false, track.TotalDiscs,
			track.Copyright, track.Publisher, track.Composer, ", ", track.ISRC,
			fmt.Sprintf("https://open.spotify.com/track/%s", track.SpotifyID),
			false, false, false,
		)

	default:
		return DownloadResult{Success: false, Error: fmt.Sprintf("unknown service: %s", service)}
	}

	if err != nil {
		return DownloadResult{Success: false, Error: err.Error(), Service: service}
	}

	// Handle already exists
	if strings.HasPrefix(filename, "EXISTS:") {
		filename = strings.TrimPrefix(filename, "EXISTS:")
	}

	return DownloadResult{Success: true, File: filename, Service: service}
}
