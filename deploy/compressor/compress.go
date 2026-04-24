// compress.go — Image & video compression tool for Blog posts
// Dependencies: ffmpeg, imagemagick (convert)
//
// Usage:
//   go run compress.go                        — process all posts in ../../Blog/posts/
//   go run compress.go ../../Blog/posts       — same, explicit posts root
//   go run compress.go ../../Blog/posts/name  — process a single post
//   go run compress.go -f [path]              — force recompress, ignoring existing output
//
// Flags:
//   -f   Force mode — clears images/ and recompresses everything from raw/
//        Without -f, files that already have a compressed output in images/ are skipped
//
// Directory structure expected:
//   posts/
//     postname/
//       images/
//         raw/    ← source files live here (never touched)
//                 ← compressed output is placed directly in images/
//
// Behavior:
//   1. Skips files whose output already exists in images/ (unless -f is set)
//   2. With -f: clears all files in images/ (not raw/, not subdirs) before compressing
//   3. Reads source files from images/raw/
//   4. Compresses images  → WebP  (EXIF orientation baked in, resized if needed)
//   5. Compresses videos  → H.264 MP4 (rotation baked in)
//   6. Places output in images/ — raw/ is left untouched

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
	maxImageWidth     = 1600 // px — images wider than this are downscaled
	videoCRF          = "23"
	videoPreset       = "slow"
	videoAudioBitrate = "128k"

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

// ── Video compression ─────────────────────────────────────────────────────────

func compressVideo(src, destDir string, force bool) result {
	filename := filepath.Base(src)
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	dest := filepath.Join(destDir, stem+".mp4")
	before := fileSize(src)

	if !force {
		if _, err := os.Stat(dest); err == nil {
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

	return result{filename: filename, before: before, after: fileSize(dest)}
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

// ── Process a single post ─────────────────────────────────────────────────────

func processPost(postDir string, force bool) (totalErrors []string) {
	imagesDir := filepath.Join(postDir, "images")
	srcDir := filepath.Join(imagesDir, "raw")

	// Verify raw/ exists and has files
	images, videos, err := collectFiles(srcDir)
	if err != nil {
		return []string{fmt.Sprintf("%s: cannot read raw/: %v", filepath.Base(postDir), err)}
	}
	total := len(images) + len(videos)
	if total == 0 {
		fmt.Printf("  %s %s — no source files found, skipping\n",
			colorize(yellow, "⚠"), filepath.Base(postDir))
		return nil
	}

	fmt.Printf("\n%s\n  Post: %s\n", hr(), filepath.Base(postDir))

	// In force mode, wipe existing output first
	if force {
		if err := clearDir(imagesDir); err != nil {
			return []string{fmt.Sprintf("%s: failed to clear images/: %v", filepath.Base(postDir), err)}
		}
		fmt.Printf("  Cleared images/ — force recompressing %d file(s) from raw/\n", total)
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
			totalErrors = append(totalErrors, filepath.Base(postDir)+"/"+r.filename+": "+r.err.Error())
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

// ── Resolve target posts ──────────────────────────────────────────────────────
// Returns a list of post directories to process based on the argument provided.
//   - No arg / posts root → all subdirectories of postsDir
//   - Single post dir     → just that directory

func resolvePostDirs(arg string) ([]string, error) {
	postsRoot, _ := filepath.Abs(defaultPostsDir)

	target := postsRoot
	if arg != "" {
		target, _ = filepath.Abs(arg)
	}

	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("'%s' is not a valid directory", target)
	}

	// If the target looks like the posts root (has no images/raw inside it directly),
	// treat it as the posts root and enumerate subdirectories.
	srcCheck := filepath.Join(target, "images", "raw")
	if _, err := os.Stat(srcCheck); os.IsNotExist(err) {
		// Treat as posts root — collect all subdirectories
		entries, err := os.ReadDir(target)
		if err != nil {
			return nil, err
		}
		var posts []string
		for _, e := range entries {
			if e.IsDir() {
				posts = append(posts, filepath.Join(target, e.Name()))
			}
		}
		if len(posts) == 0 {
			return nil, fmt.Errorf("no post subdirectories found in %s", target)
		}
		return posts, nil
	}

	// Target is a single post directory
	return []string{target}, nil
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

	postDirs, err := resolvePostDirs(arg)
	if err != nil {
		fmt.Fprintln(os.Stderr, colorize(red, "✗ "+err.Error()))
		os.Exit(1)
	}

	mode := "incremental"
	if *force {
		mode = "force"
	}
	fmt.Printf("\n%s\n  compress [%s] — %d post(s) to process\n", hr(), mode, len(postDirs))

	var allErrors []string
	for _, dir := range postDirs {
		errs := processPost(dir, *force)
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
