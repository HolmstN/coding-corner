package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var participantName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func discover(root string, changedPaths []string) ([]string, error) {
	seen := make(map[string]bool)
	for _, path := range changedPaths {
		parts := strings.Split(path, "/")
		if len(parts) < 4 || parts[0] != "submissions" || parts[1] != "tcp-gateway" {
			continue
		}
		if !participantName.MatchString(parts[2]) {
			return nil, fmt.Errorf("invalid participant directory %q: use letters, digits, hyphens, or underscores", parts[2])
		}
		seen[strings.Join(parts[:3], "/")] = true
	}
	submissions := make([]string, 0, len(seen))
	for path := range seen {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		if os.IsNotExist(err) {
			continue // A completely removed submission has nothing left to run.
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s must be a directory, not a symlink or file", path)
		}
		launcher, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path), "run.sh"))
		if err != nil {
			return nil, fmt.Errorf("%s must contain run.sh: %w", path, err)
		}
		if !launcher.Mode().IsRegular() {
			return nil, fmt.Errorf("%s/run.sh must be a regular file", path)
		}
		submissions = append(submissions, path)
	}
	sort.Strings(submissions)
	return submissions, nil
}

func main() {
	base := flag.String("base", "", "base commit to compare with HEAD")
	output := flag.String("output", "", "GitHub Actions output file")
	flag.Parse()
	if *base == "" {
		log.Fatal("--base is required")
	}
	cmd := exec.Command("git", "diff", "--no-renames", "--name-only", "-z", *base, "HEAD", "--", "submissions/tcp-gateway/")
	data, err := cmd.Output()
	if err != nil {
		log.Fatalf("find changed submissions: %v", err)
	}
	submissions, err := discover(".", strings.Split(string(data), "\x00"))
	if err != nil {
		log.Fatal(err)
	}
	matrix, err := json.Marshal(struct {
		Submission []string `json:"submission"`
	}{submissions})
	if err != nil {
		log.Fatal(err)
	}
	result := fmt.Sprintf("matrix=%s\nhas_submissions=%t\n", matrix, len(submissions) > 0)
	if *output == "" {
		fmt.Print(result)
		return
	}
	file, err := os.OpenFile(*output, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := file.WriteString(result); err != nil {
		file.Close()
		log.Fatal(err)
	}
	if err := file.Close(); err != nil {
		log.Fatal(err)
	}
}
