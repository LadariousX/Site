// compress.go — Image & video compression tool
// Dependencies: ffmpeg, imagemagick (convert)
//
// Usage:
//   go run compress.go                          — process all posts in ../../Blog/posts/
//   go run compress.go ../../Blog/posts         — same, explicit posts root
//   go run compress.go ../../Blog/posts/name/images — process one images dir
//   go run compress.go ../../Blog/static/images — process static images dir
//   go run compress.go -f [path]                — force recompress, ignoring existing output
//
// Flags:
//   -f   Force mode — clears output dir and recompresses everything from raw/
//        Without -f, files that already have a compressed output are skipped
//
// Accepted directory layouts:
//
//   Point directly to an images dir:
//     images/
//       raw/    ← source files
//               ← compressed output placed here
//
//   Point to a parent dir — each subdir that has images/ with raw/ inside is processed:
//     posts/
//       postname/
//         images/
//           raw/
//
// Behavior:
//   1. Skips files whose output already exists (unless -f is set)
//   2. With -f: clears all files in the output dir (not raw/, not subdirs) before compressing
//   3. Reads source files from raw/
//   4. Compresses images  → WebP  (EXIF orientation baked in, resized if needed)
//   5. Compresses videos  → H.264 MP4 (rotation baked in)
//   6. Places output alongside raw/ — raw/ is left untouched

package main

import (
	"flag"
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
	maxImageWidth     = 1000 // px — images wider than this are downscaled
	videoCRF          = "23"
	videoPreset       = "slow"
	videoAudioBitrate = "128k"

	posterQuality  = 80
	posterMaxWidth = 600 // px — video poster thumbnails downscaled to this width
	postersSubdir  = "thumbs"

	defaultPostsDir = "/Users/layden/Development/Site/Blog/posts"
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
	skipped  bool
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

func exifOrientation(src string) int {
	out, err := exec.Command("ffprobe",
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "stream_tags=rotate",
		"-of", "default=noprint_wrappers=1:nokey=1",
		src,
	).Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
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

func rotateArgs(degrees int) (string, bool) {
	degrees = ((degrees % 360) + 360) % 360
	switch degrees {
	case 90:
		return "transpose=1", true
	case 180:
		return "transpose=2,transpose=2", true
	case 270:
		return "transpose=2", true
	default:
		return "", false
	}
}

// ── Image compression ─────────────────────────────────────────────────────────

func compressImage(src, destDir string, force bool) result {
	filename := filepath.Base(src)
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	dest := filepath.Join(destDir, stem+".webp")
	before := fileSize(src)

	if !force {
		if _, err := os.Stat(dest); err == nil {
			return result{filename: filename, skipped: true}
		}
	}

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

	return result{filename: filename, before: before, after: fileSize(dest)}
}

// ── Video poster thumbnails ───────────────────────────────────────────────────
// Extracts a single frame from a compressed video as a WebP poster image, so
// gallery pages can show a static thumbnail instead of loading video data
// client-side. Posters are written to <destDir>/thumbs/<stem>.webp.

func generatePoster(video, destDir, stem string, force bool) error {
	posterDir := filepath.Join(destDir, postersSubdir)
	if err := os.MkdirAll(posterDir, 0755); err != nil {
		return err
	}
	dest := filepath.Join(posterDir, stem+".webp")

	if !force {
		if _, err := os.Stat(dest); err == nil {
			return nil
		}
	}

	cmd := exec.Command("ffmpeg",
		"-y",
		"-ss", "0.1",
		"-i", video,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", posterMaxWidth),
		"-c:v", "libwebp",
		"-quality", strconv.Itoa(posterQuality),
		dest,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(dest)
		return fmt.Errorf("ffmpeg (poster): %w\n%s", err, string(out))
	}
	return nil
}

// ── Video compression ─────────────────────────────────────────────────────────

func compressVideo(src, destDir string, force bool) result {
	filename := filepath.Base(src)
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	dest := filepath.Join(destDir, stem+".mp4")
	before := fileSize(src)

	if !force {
		if _, err := os.Stat(dest); err == nil {
			if err := generatePoster(dest, destDir, stem, force); err != nil {
				return result{filename: filename, err: err}
			}
			return result{filename: filename, skipped: true}
		}
	}

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
		"-map_metadata", "-1",
		"-y", dest,
	)

	cmd := exec.Command("ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(dest)
		return result{filename: filename, before: before,
			err: fmt.Errorf("ffmpeg: %w\n%s", err, string(out))}
	}

	if err := generatePoster(dest, destDir, stem, force); err != nil {
		return result{filename: filename, before: before, after: fileSize(dest), err: err}
	}

	return result{filename: filename, before: before, after: fileSize(dest)}
}

// ── Collect files ─────────────────────────────────────────────────────────────

func collectFiles(dir string) (images, videos []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	imageExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".heic": true}
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

// ── Clear images/ directory ───────────────────────────────────────────────────
// Removes all files directly inside dir, leaving subdirectories (e.g. raw/) intact.

func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ── Process an images directory ───────────────────────────────────────────────

// imagesDirLabel returns a display label for an images dir. When the dir is
// named "images" the parent name is prepended to distinguish multiple targets.
func imagesDirLabel(path string) string {
	base := filepath.Base(path)
	if base == "images" {
		return filepath.Base(filepath.Dir(path)) + "/images"
	}
	return base
}

func processImagesDir(imagesDir string, force bool) (totalErrors []string) {
	label := imagesDirLabel(imagesDir)
	srcDir := filepath.Join(imagesDir, "raw")

	images, videos, err := collectFiles(srcDir)
	if err != nil {
		return []string{fmt.Sprintf("%s: cannot read raw/: %v", label, err)}
	}
	total := len(images) + len(videos)
	if total == 0 {
		fmt.Printf("  %s %s — no source files found, skipping\n",
			colorize(yellow, "⚠"), label)
		return nil
	}

	fmt.Printf("\n%s\n  %s\n", hr(), label)

	if force {
		if err := clearDir(imagesDir); err != nil {
			return []string{fmt.Sprintf("%s: failed to clear output dir: %v", label, err)}
		}
		fmt.Printf("  Cleared output — force recompressing %d file(s) from raw/\n", total)
	} else {
		fmt.Printf("  Checking %d file(s) from raw/ — skipping existing output\n", total)
	}

	results := make(chan result, total)
	var wg sync.WaitGroup

	if len(images) > 0 {
		fmt.Println(colorize(bold, "  Images → WebP"))
		for _, f := range images {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				results <- compressImage(path, imagesDir, force)
			}(f)
		}
	}

	if len(videos) > 0 {
		fmt.Println(colorize(bold, "  Video → H.264 MP4"))
		for _, f := range videos {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				results <- compressVideo(path, imagesDir, force)
			}(f)
		}
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var mu sync.Mutex
	for r := range results {
		mu.Lock()
		switch {
		case r.err != nil:
			fmt.Printf("  %s %-40s %s\n",
				colorize(red, "✗"), r.filename, colorize(red, r.err.Error()))
			totalErrors = append(totalErrors, label+"/"+r.filename+": "+r.err.Error())
		case r.skipped:
			fmt.Printf("  %s %-40s already exists\n",
				colorize(yellow, "–"), r.filename)
		default:
			fmt.Printf("  %s %-40s %s → %s\n",
				colorize(green, "✓"), r.filename, humanSize(r.before), humanSize(r.after))
		}
		mu.Unlock()
	}

	return totalErrors
}

// ── Resolve targets ───────────────────────────────────────────────────────────
// Returns a list of images directories (each containing raw/) to process.
//   - Target has raw/ directly → single images dir
//   - Otherwise → enumerate subdirs; collect those whose images/ subdir has raw/

func resolveTargets(arg string) ([]string, error) {
	postsRoot, _ := filepath.Abs(defaultPostsDir)

	target := postsRoot
	if arg != "" {
		target, _ = filepath.Abs(arg)
	}

	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("'%s' is not a valid directory", target)
	}

	// Target is an images dir (raw/ lives directly inside)
	if _, err := os.Stat(filepath.Join(target, "raw")); err == nil {
		return []string{target}, nil
	}

	// Enumerate subdirs and collect their images/ dirs that have raw/
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		imagesDir := filepath.Join(target, e.Name(), "images")
		if _, err := os.Stat(filepath.Join(imagesDir, "raw")); err == nil {
			dirs = append(dirs, imagesDir)
		}
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no images/ directories with raw/ found under %s", target)
	}
	return dirs, nil
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	force := flag.Bool("f", false, "force recompress — clear images/ and reprocess all files from raw/")
	flag.Parse()

	arg := flag.Arg(0)

	if err := checkDeps(); err != nil {
		fmt.Fprintln(os.Stderr, colorize(red, "✗ "+err.Error()))
		os.Exit(1)
	}

	imageDirs, err := resolveTargets(arg)
	if err != nil {
		fmt.Fprintln(os.Stderr, colorize(red, "✗ "+err.Error()))
		os.Exit(1)
	}

	mode := "incremental"
	if *force {
		mode = "force"
	}
	fmt.Printf("\n%s\n  compress [%s] — %d target(s)\n", hr(), mode, len(imageDirs))

	var allErrors []string
	for _, dir := range imageDirs {
		errs := processImagesDir(dir, *force)
		allErrors = append(allErrors, errs...)
	}

	fmt.Println(hr())
	if len(allErrors) == 0 {
		fmt.Println(colorize(green, "  Done.") + "\n")
	} else {
		fmt.Printf("%s %d error(s):\n", colorize(yellow, "  Done with"), len(allErrors))
		for _, e := range allErrors {
			fmt.Println("    -", e)
		}
		fmt.Println()
		os.Exit(1)
	}
}
