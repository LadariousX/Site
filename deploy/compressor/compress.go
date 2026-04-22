// compress.go — Image & video compression tool
// Dependencies: ffmpeg, imagemagick (convert)
// Usage: go run compress.go <directory>
//        go build -o compress && ./compress <directory>
//
// Images (JPEG/PNG) → WebP  — EXIF orientation is baked in before conversion
// Videos (MP4/MOV)  → H.264 MP4 — rotation metadata is applied via transpose filter
// Originals are moved to <directory>/source/

package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ── Config ────────────────────────────────────────────────────────────────────

const (
	webpQuality       = 82
	maxImageWidth     = 1600 // px — images wider than this are downscaled
	videoCRF          = "23"
	videoPreset       = "slow"
	videoAudioBitrate = "128k"
)

// ── ANSI colors ───────────────────────────────────────────────────────────────

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
)

func colorize(color, s string) string { return color + s + reset }
func hr() string                      { return strings.Repeat("─", 60) }

// ── Result ────────────────────────────────────────────────────────────────────

type result struct {
	filename string
	before   int64
	after    int64
	err      error
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func humanSize(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB"}
	i := int(math.Log(float64(bytes)) / math.Log(1024))
	if i >= len(units) {
		i = len(units) - 1
	}
	val := float64(bytes) / math.Pow(1024, float64(i))
	if i == 0 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f %s", val, units[i])
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func checkDeps() error {
	missing := []string{}
	for _, dep := range []string{"ffmpeg", "convert"} {
		if _, err := exec.LookPath(dep); err != nil {
			missing = append(missing, dep)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing dependencies: %s\n\n  macOS:  brew install %s imagemagick\n  Ubuntu: sudo apt install %s imagemagick",
			strings.Join(missing, ", "),
			strings.Join(missing, " "),
			strings.Join(missing, " "),
		)
	}
	return nil
}

// ── EXIF orientation ──────────────────────────────────────────────────────────
// Reads the EXIF orientation tag via ffprobe so we have no extra deps.
// Returns 1 (normal) if unreadable.

func exifOrientation(src string) int {
	out, err := exec.Command("ffprobe",
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "stream_tags=rotate",
		"-of", "default=noprint_wrappers=1:nokey=1",
		src,
	).Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		// Try side_data rotation (MOV/MP4 display matrix)
		out, err = exec.Command("ffprobe",
			"-v", "quiet",
			"-select_streams", "v:0",
			"-show_entries", "stream_side_data=rotation",
			"-of", "default=noprint_wrappers=1:nokey=1",
			src,
		).Output()
		if err != nil {
			return 0
		}
	}
	deg, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return deg
}

// rotateArgs returns the ffmpeg vf filter string needed to bake in rotation,
// and whether any rotation is actually needed.
func rotateArgs(degrees int) (string, bool) {
	// Normalize to 0–359
	degrees = ((degrees % 360) + 360) % 360
	switch degrees {
	case 90:
		return "transpose=1", true // 90° clockwise
	case 180:
		return "transpose=2,transpose=2", true // 180°
	case 270:
		return "transpose=2", true // 90° counter-clockwise
	default:
		return "", false
	}
}

// ── Image compression ─────────────────────────────────────────────────────────
// Pipeline:
//  1. convert (ImageMagick): -auto-orient bakes EXIF orientation into pixels,
//     -resize caps width at maxImageWidth, -strip removes all metadata.
//  2. ImageMagick convert writes directly to WebP (built-in delegate).
//  3. Move original to source/.
//
// ImageMagick is used instead of ffmpeg because ffmpeg's EXIF auto-rotate is
// inconsistent for still images across versions. ImageMagick -auto-orient
// correctly handles all 8 EXIF orientation variants.

func compressImage(src, sourceDir string) result {
	filename := filepath.Base(src)
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	dest := filepath.Join(filepath.Dir(src), stem+".webp")
	before := fileSize(src)

	// Step 1: ImageMagick — auto-orient + resize → temp PNG.
	// -auto-orient   reads EXIF Orientation tag and physically rotates pixels
	// -resize Wx>    shrinks if wider than maxImageWidth, never upscales (> flag)
	// -strip         removes all metadata after orientation is baked in
	// ImageMagick 6: operator flags must come BEFORE the source file.
	// Convert directly to WebP using IM's built-in WebP delegate —
	// no intermediate PNG needed.
	resizeFlag := fmt.Sprintf("%dx>", maxImageWidth)
	convertCmd := exec.Command("convert",
		"-auto-orient",
		"-strip",
		src,
		"-resize", resizeFlag,
		"-quality", strconv.Itoa(webpQuality),
		dest,
	)
	if out, err := convertCmd.CombinedOutput(); err != nil {
		os.Remove(dest)
		return result{filename: filename, before: before,
			err: fmt.Errorf("convert (ImageMagick): %w\n%s", err, string(out))}
	}

	// Step 3: move original to source/
	if err := os.Rename(src, filepath.Join(sourceDir, filename)); err != nil {
		return result{filename: filename, before: before,
			err: fmt.Errorf("move to source: %w", err)}
	}

	return result{filename: filename, before: before, after: fileSize(dest)}
}

// ── Video compression ─────────────────────────────────────────────────────────
// Pipeline:
//  1. Probe rotation metadata.
//  2. Build a -vf transpose filter if needed to bake rotation into pixels.
//  3. Re-encode to H.264/AAC MP4 with faststart; strip all metadata.
//  4. Move original to source/.

func compressVideo(src, sourceDir string) result {
	filename := filepath.Base(src)
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	dest := filepath.Join(filepath.Dir(src), stem+".mp4")
	before := fileSize(src)

	// If source is already .mp4, write to a temp name first
	isSamePath := strings.EqualFold(src, dest)
	actualDest := dest
	if isSamePath {
		actualDest = filepath.Join(filepath.Dir(src), stem+"_compressed.mp4")
	}

	// Build vf filter: bake rotation if present
	degrees := exifOrientation(src)
	vfFilter, needsRotate := rotateArgs(degrees)

	args := []string{"-i", src}

	if needsRotate {
		args = append(args, "-vf", vfFilter)
	}

	args = append(args,
		"-c:v", "libx264",
		"-crf", videoCRF,
		"-preset", videoPreset,
		"-c:a", "aac",
		"-b:a", videoAudioBitrate,
		"-movflags", "+faststart",
		"-map_metadata", "-1", // strip all metadata (rotation tag gone, pixels are correct)
		"-y", actualDest,
	)

	cmd := exec.Command("ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(actualDest)
		return result{filename: filename, before: before,
			err: fmt.Errorf("ffmpeg: %w\n%s", err, string(out))}
	}

	after := fileSize(actualDest)

	// Move original to source/
	if err := os.Rename(src, filepath.Join(sourceDir, filename)); err != nil {
		os.Remove(actualDest)
		return result{filename: filename, before: before,
			err: fmt.Errorf("move to source: %w", err)}
	}

	// Rename temp → final if needed
	if isSamePath {
		if err := os.Rename(actualDest, dest); err != nil {
			return result{filename: filename, before: before,
				err: fmt.Errorf("rename compressed: %w", err)}
		}
	}

	return result{filename: filename, before: before, after: after}
}

// ── HTML src rewriting ────────────────────────────────────────────────────────
// Walks the target directory (non-recursively) for .html files and replaces
// any src/href references to .jpg/.jpeg/.png with .webp equivalents.

var imageExtReplacer = strings.NewReplacer(
	".jpg\"", ".webp\"",
	".jpg'", ".webp'",
	".jpeg\"", ".webp\"",
	".jpeg'", ".webp'",
	".png\"", ".webp\"",
	".png'", ".webp'",
	// URL-encoded variants (just in case)
	".jpg%22", ".webp%22",
	".jpeg%22", ".webp%22",
	".png%22", ".webp%22",
)

func rewriteHTMLRefs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var rewritten []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".html") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return rewritten, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		original := string(data)
		updated := imageExtReplacer.Replace(original)
		if updated == original {
			continue // nothing changed
		}
		if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
			return rewritten, fmt.Errorf("write %s: %w", e.Name(), err)
		}
		rewritten = append(rewritten, e.Name())
	}
	return rewritten, nil
}

// ── Collect files ─────────────────────────────────────────────────────────────

func collectFiles(dir string) (images, videos []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	imageExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true}
	videoExts := map[string]bool{".mp4": true, ".mov": true}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		full := filepath.Join(dir, e.Name())
		if imageExts[ext] {
			images = append(images, full)
		} else if videoExts[ext] {
			videos = append(videos, full)
		}
	}
	return
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: compress <directory>")
		os.Exit(1)
	}

	targetDir := os.Args[1]
	if info, err := os.Stat(targetDir); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: '%s' is not a valid directory.\n", targetDir)
		os.Exit(1)
	}

	if err := checkDeps(); err != nil {
		fmt.Fprintln(os.Stderr, colorize(red, "✗ "+err.Error()))
		os.Exit(1)
	}

	sourceDir := filepath.Join(targetDir, "source")

	// If a source/ folder already exists, ask the user whether to re-compress
	// those originals instead of (or in addition to) any new files in targetDir.
	if info, err := os.Stat(sourceDir); err == nil && info.IsDir() {
		srcImages, srcVideos, err := collectFiles(sourceDir)
		if err == nil && len(srcImages)+len(srcVideos) > 0 {
			fmt.Println()
			fmt.Printf("%s source/ folder detected with %d file(s).",
				colorize(yellow, "  ⚠"), len(srcImages)+len(srcVideos))
			fmt.Print("  Delete compressed files and re-compress originals? [y/N] ")

			var answer string
			fmt.Scanln(&answer)
			answer = strings.ToLower(strings.TrimSpace(answer))

			if answer == "y" || answer == "yes" {
				// Remove all current .webp and .mp4 files in targetDir
				entries, _ := os.ReadDir(targetDir)
				removed := 0
				for _, e := range entries {
					if e.IsDir() {
						continue
					}
					ext := strings.ToLower(filepath.Ext(e.Name()))
					if ext == ".webp" || ext == ".mp4" {
						os.Remove(filepath.Join(targetDir, e.Name()))
						removed++
					}
				}
				// Move source/ files back to targetDir
				for _, f := range append(srcImages, srcVideos...) {
					dest := filepath.Join(targetDir, filepath.Base(f))
					if err := os.Rename(f, dest); err != nil {
						fmt.Fprintf(os.Stderr, "  Failed to restore %s: %v", filepath.Base(f), err)
					}
				}
				fmt.Printf("  Removed %d compressed file(s), restored %d original(s).",
					removed, len(srcImages)+len(srcVideos))
			} else {
				fmt.Println("  Skipping — will compress any new files only.")
			}
			fmt.Println()
		}
	}

	images, videos, err := collectFiles(targetDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading directory:", err)
		os.Exit(1)
	}

	total := len(images) + len(videos)
	if total == 0 {
		fmt.Println(colorize(yellow, "  No JPEG, PNG, MOV, or MP4 files found in "+targetDir))
		os.Exit(0)
	}

	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		fmt.Fprintln(os.Stderr, "Error creating source dir:", err)
		os.Exit(1)
	}

	absTarget, _ := filepath.Abs(targetDir)
	fmt.Println()
	fmt.Printf("%s\n  Target: %s\n", colorize(bold, "  compress"), absTarget)
	fmt.Println(hr())

	results := make(chan result, total)
	var wg sync.WaitGroup

	// Launch image goroutines
	if len(images) > 0 {
		fmt.Println(colorize(bold, "  Images → WebP"))
		for _, f := range images {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				results <- compressImage(path, sourceDir)
			}(f)
		}
	}

	// Launch video goroutines
	if len(videos) > 0 {
		fmt.Println(colorize(bold, "  Video → H.264 MP4"))
		for _, f := range videos {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				results <- compressVideo(path, sourceDir)
			}(f)
		}
	}

	// Close results channel once all goroutines finish
	go func() {
		wg.Wait()
		close(results)
	}()

	// Print results as they arrive; tally errors
	var mu sync.Mutex
	var errors []string

	// Collect and print inline as channel drains
	// (channel is already buffered so goroutines never block)
	for r := range results {
		mu.Lock()
		if r.err != nil {
			fmt.Printf("  %s %-40s %s\n",
				colorize(red, "✗"),
				r.filename,
				colorize(red, r.err.Error()),
			)
			errors = append(errors, r.filename+": "+r.err.Error())
		} else {
			fmt.Printf("  %s %-40s %s → %s\n",
				colorize(green, "✓"),
				r.filename,
				humanSize(r.before),
				humanSize(r.after),
			)
		}
		mu.Unlock()
	}

	// Rewrite HTML refs from .jpg/.jpeg/.png → .webp
	rewritten, htmlErr := rewriteHTMLRefs(targetDir)
	if len(rewritten) > 0 {
		fmt.Println()
		fmt.Println(colorize(bold, "  HTML src rewrites"))
		for _, f := range rewritten {
			fmt.Printf("  %s %s\n", colorize(green, "✓"), f)
		}
	}

	fmt.Println(hr())
	if len(errors) == 0 && htmlErr == nil {
		fmt.Printf("%s Originals saved to: %s\n\n",
			colorize(green, "  Done."), sourceDir)
	} else {
		if htmlErr != nil {
			errors = append(errors, "html rewrite: "+htmlErr.Error())
		}
		fmt.Printf("%s %d error(s):\n", colorize(yellow, "  Done with"), len(errors))
		for _, e := range errors {
			fmt.Println("    -", e)
		}
		fmt.Println()
		os.Exit(1)
	}
}
