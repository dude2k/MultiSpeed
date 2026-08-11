// Command generate-go-license-bundle copies license and notice files for the
// exact Go modules used by a build target into a deterministic directory tree.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type packageDescription struct {
	ImportPath string             `json:"ImportPath"`
	Standard   bool               `json:"Standard"`
	Module     *moduleDescription `json:"Module"`
}

type moduleDescription struct {
	Path    string             `json:"Path"`
	Version string             `json:"Version"`
	Dir     string             `json:"Dir"`
	Main    bool               `json:"Main"`
	Replace *moduleDescription `json:"Replace"`
}

type manifest struct {
	Format  string          `json:"format"`
	Modules []manifestEntry `json:"modules"`
}

type manifestEntry struct {
	Path    string         `json:"path"`
	Version string         `json:"version"`
	Files   []manifestFile `json:"files"`
}

type manifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func main() {
	if len(os.Args) < 3 {
		fatal(errors.New("usage: generate-go-license-bundle <output> <go-list-target>"))
	}
	output, err := filepath.Abs(os.Args[1])
	if err != nil {
		fatal(err)
	}
	modules, err := buildModules(os.Args[2:])
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		fatal(err)
	}

	entries := make([]manifestEntry, 0, len(modules)+1)
	standardEntry, err := copyStandardLibraryNotices(output)
	if err != nil {
		fatal(err)
	}
	entries = append(entries, standardEntry)

	paths := make([]string, 0, len(modules))
	for modulePath := range modules {
		paths = append(paths, modulePath)
	}
	sort.Strings(paths)
	for _, modulePath := range paths {
		entry, copyErr := copyModuleNotices(output, modules[modulePath])
		if copyErr != nil {
			fatal(copyErr)
		}
		entries = append(entries, entry)
	}

	data, err := json.MarshalIndent(manifest{Format: "multispeed-go-license-bundle-v1", Modules: entries}, "", "  ")
	if err != nil {
		fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(output, "manifest.json"), data, 0o644); err != nil {
		fatal(err)
	}
}

func buildModules(targets []string) (map[string]moduleDescription, error) {
	arguments := append([]string{"list", "-deps", "-json"}, targets...)
	command := exec.Command("go", arguments...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(stdout)
	modules := make(map[string]moduleDescription)
	for {
		var pkg packageDescription
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if pkg.Standard || pkg.Module == nil || pkg.Module.Main {
			continue
		}
		module := *pkg.Module
		if module.Replace != nil {
			replacement := *module.Replace
			replacement.Path = module.Path
			if replacement.Version == "" {
				replacement.Version = module.Version
			}
			module = replacement
		}
		if module.Path == "" || module.Dir == "" {
			return nil, fmt.Errorf("module metadata is incomplete for package %s", pkg.ImportPath)
		}
		modules[module.Path] = module
	}
	if err := command.Wait(); err != nil {
		return nil, err
	}
	return modules, nil
}

func copyStandardLibraryNotices(output string) (manifestEntry, error) {
	entry := manifestEntry{Path: "go.dev/stdlib", Version: runtime.Version()}
	rootOutput, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return entry, fmt.Errorf("locate Go standard library: %w", err)
	}
	root := strings.TrimSpace(string(rootOutput))
	if root == "" || !filepath.IsAbs(root) {
		return entry, errors.New("go standard library path is invalid")
	}
	for _, name := range []string{"LICENSE", "PATENTS"} {
		source := filepath.Join(root, name)
		if _, err := os.Stat(source); err != nil {
			return entry, fmt.Errorf("inspect Go standard library %s: %w", name, err)
		}
		file, err := copyNotice(output, safeName(entry.Path)+"@"+safeName(entry.Version), root, source)
		if err != nil {
			return entry, err
		}
		entry.Files = append(entry.Files, file)
	}
	return entry, nil
}

func copyModuleNotices(output string, module moduleDescription) (manifestEntry, error) {
	entry := manifestEntry{Path: module.Path, Version: module.Version}
	notices, err := findNotices(module.Dir)
	if err != nil {
		return entry, err
	}
	if len(notices) == 0 {
		return entry, fmt.Errorf("go module has no license or notice file: %s@%s", module.Path, module.Version)
	}
	destination := safeName(module.Path) + "@" + safeName(module.Version)
	for _, source := range notices {
		file, err := copyNotice(output, destination, module.Dir, source)
		if err != nil {
			return entry, err
		}
		entry.Files = append(entry.Files, file)
	}
	return entry, nil
}

func findNotices(root string) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root {
			name := strings.ToLower(entry.Name())
			if name == ".git" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !noticeName(entry.Name()) {
			return nil
		}
		matches = append(matches, path)
		return nil
	})
	sort.Strings(matches)
	return matches, err
}

func noticeName(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"license", "licence", "copying", "notice", "copyright"} {
		if lower == prefix || strings.HasPrefix(lower, prefix+".") || strings.HasPrefix(lower, prefix+"-") || strings.HasPrefix(lower, prefix+"_") {
			return true
		}
	}
	return false
}

func copyNotice(output, destinationName, root, source string) (manifestFile, error) {
	relative, err := filepath.Rel(root, source)
	if err != nil {
		return manifestFile{}, err
	}
	destination := filepath.Join(output, destinationName, relative)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return manifestFile{}, err
	}
	input, err := os.Open(source)
	if err != nil {
		return manifestFile{}, err
	}
	defer func() { _ = input.Close() }()
	data, err := io.ReadAll(input)
	if err != nil {
		return manifestFile{}, err
	}
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		return manifestFile{}, err
	}
	digest := sha256.Sum256(data)
	return manifestFile{Path: filepath.ToSlash(relative), SHA256: hex.EncodeToString(digest[:])}, nil
}

func safeName(value string) string {
	replacer := strings.NewReplacer("/", "__", "\\", "__", ":", "_")
	return replacer.Replace(value)
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
