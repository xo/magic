package oracle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// BinwalkMatch is one match of binwalk, from the JSON of its option -l.
type BinwalkMatch struct {
	ID          string `json:"id"`
	Offset      int    `json:"offset"`
	Size        int    `json:"size"`
	Name        string `json:"name"`
	Confidence  int    `json:"confidence"`
	Description string `json:"description"`
}

// Binwalk runs the binwalk that test/oracle/build-binwalk.sh builds (D49).
type Binwalk struct {
	path string
}

// BinwalkPath returns the path of the binwalk of the oracle.
func BinwalkPath() string {
	return filepath.Join(Dir(), "..", ".binwalk", "bin", "binwalk")
}

// OpenBinwalk returns the binwalk of the oracle. It skips t when the binary
// is not built.
func OpenBinwalk(t testing.TB) *Binwalk {
	t.Helper()
	p := BinwalkPath()
	if _, err := os.Stat(p); err != nil {
		t.Skipf("no binwalk in %s. Run test/oracle/build-binwalk.sh", p)
	}
	return &Binwalk{path: p}
}

// Scan runs binwalk on the file at path, for the signatures of include only,
// and returns its matches. With all, it tries the short signatures at every
// offset.
//
// Under load, binwalk can close its list of JSON before its worker logs the
// result, and print only "]". Scan then runs it again, up to five times.
func (b *Binwalk) Scan(path string, include []string, all bool) ([]BinwalkMatch, error) {
	var err error
	for range 5 {
		var ms []BinwalkMatch
		if ms, err = b.scan(path, include, all); !errors.Is(err, ErrBadJSON) {
			return ms, err
		}
	}
	return nil, err
}

// ErrBadJSON is the error of a run of binwalk whose JSON cannot be read.
var ErrBadJSON = errors.New("binwalk printed JSON that cannot be read")

// scan runs binwalk once.
func (b *Binwalk) scan(path string, include []string, all bool) ([]BinwalkMatch, error) {
	args := []string{"-q", "-l", "-"}
	if len(include) > 0 {
		// -y takes many values, and takes the path as one more, so the
		// list goes after an equals sign.
		args = append(args, "--include="+strings.Join(include, ","))
	}
	if all {
		args = append(args, "-a")
	}
	args = append(args, path)
	cmd := exec.Command(b.path, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running binwalk on %s: %w: %s", path, err, stderr.String())
	}
	var res []struct {
		Analysis struct {
			FileMap []BinwalkMatch `json:"file_map"`
		}
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("%w: on %s: %w", ErrBadJSON, path, err)
	}
	if len(res) != 1 {
		return nil, fmt.Errorf("binwalk on %s: expected one analysis, got: %d", path, len(res))
	}
	return res[0].Analysis.FileMap, nil
}

// Extract runs binwalk with -e on the file at path, for the signatures of
// include only, and writes the extraction under out. binwalk writes the
// files of the match at an offset under out/<name>.extracted/<HEX offset>,
// where name is the base name of path.
//
// binwalk can exit before its worker starts, and then it extracts nothing
// and logs no analysis. Extract logs the JSON to a file, and runs binwalk
// again, up to five times, while the log holds no analysis.
func (b *Binwalk) Extract(path, out string, include []string) error {
	var err error
	for range 5 {
		if err = os.RemoveAll(out); err != nil {
			return fmt.Errorf("removing %s: %w", out, err)
		}
		if err = b.extract(path, out, include); !errors.Is(err, ErrBadJSON) {
			return err
		}
	}
	return err
}

// extract runs binwalk -e once.
func (b *Binwalk) extract(path, out string, include []string) error {
	log := out + ".json"
	defer func() { _ = os.Remove(log) }()
	args := []string{"-q", "-e", "-d", out, "-l", log}
	if len(include) > 0 {
		args = append(args, "--include="+strings.Join(include, ","))
	}
	args = append(args, path)
	cmd := exec.Command(b.path, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running binwalk -e on %s: %w: %s", path, err, stderr.String())
	}
	data, err := os.ReadFile(log)
	if err != nil {
		return fmt.Errorf("%w: on %s: %w", ErrBadJSON, path, err)
	}
	var res []json.RawMessage
	if err := json.Unmarshal(data, &res); err != nil || len(res) != 1 {
		return fmt.Errorf("%w: on %s: no analysis", ErrBadJSON, path)
	}
	return nil
}

// Extraction is the result of one extraction of binwalk.
type Extraction struct {
	Success         bool   `json:"success"`
	DoNotRecurse    bool   `json:"do_not_recurse"`
	OutputDirectory string `json:"output_directory"`
}

// Analysis is the analysis of one file by binwalk, with its extractions by
// the identifier of each match.
type Analysis struct {
	FilePath    string                `json:"file_path"`
	FileMap     []BinwalkMatch        `json:"file_map"`
	Extractions map[string]Extraction `json:"extractions"`
}

// Recurse walks the file at path as binwalk -M does, to depth: it extracts
// each match, and walks each regular file that is not empty of each
// extraction that succeeded and whose extractor does not stop the walk. It
// returns the matches of each file that has some, by a path: "input" for
// the file, and <path>.extracted/<HEX offset>/<file> below it.
//
// It runs binwalk -e on one file at a time, and does the walk itself. The
// main loop of binwalk -M exits when its last job ends within the sleep of
// the loop, before it reads the result of that job, and so it loses the
// analysis of a small last file.
func (b *Binwalk) Recurse(path, work string, include []string, depth int) (map[string][]BinwalkMatch, error) {
	type file struct {
		path, key string
		depth     int
	}
	out := map[string][]BinwalkMatch{}
	queue := []file{{path, "input", 0}}
	for n := 0; len(queue) > 0; n++ {
		f := queue[0]
		queue = queue[1:]
		a, err := b.analyze(f.path, filepath.Join(work, fmt.Sprint(n)), include)
		if err != nil {
			return nil, err
		}
		if len(a.FileMap) > 0 {
			out[f.key] = a.FileMap
		}
		if f.depth >= depth {
			continue
		}
		for _, m := range a.FileMap {
			ex, ok := a.Extractions[m.ID]
			if !ok || !ex.Success || ex.DoNotRecurse {
				continue
			}
			err := filepath.WalkDir(ex.OutputDirectory, func(p string, d fs.DirEntry, err error) error {
				if err != nil || !d.Type().IsRegular() {
					return err
				}
				fi, err := d.Info()
				if err != nil || fi.Size() == 0 {
					return err
				}
				rel, err := filepath.Rel(ex.OutputDirectory, p)
				if err != nil {
					return err
				}
				key := fmt.Sprintf("%s.extracted/%X/%s", f.key, m.Offset, filepath.ToSlash(rel))
				queue = append(queue, file{p, key, f.depth + 1})
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("listing %s: %w", ex.OutputDirectory, err)
			}
		}
	}
	return out, nil
}

// analyze runs binwalk -e on one file, and returns its analysis. It runs
// binwalk again, up to five times, when binwalk exits before its worker
// starts.
func (b *Binwalk) analyze(path, out string, include []string) (Analysis, error) {
	var err error
	for range 5 {
		var a Analysis
		if a, err = b.analyzeOnce(path, out, include); !errors.Is(err, ErrBadJSON) {
			return a, err
		}
	}
	return Analysis{}, err
}

func (b *Binwalk) analyzeOnce(path, out string, include []string) (Analysis, error) {
	if err := os.RemoveAll(out); err != nil {
		return Analysis{}, fmt.Errorf("removing %s: %w", out, err)
	}
	log := out + ".json"
	defer func() { _ = os.Remove(log) }()
	args := []string{"-q", "-e", "-d", out, "-l", log}
	if len(include) > 0 {
		args = append(args, "--include="+strings.Join(include, ","))
	}
	args = append(args, path)
	cmd := exec.Command(b.path, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Analysis{}, fmt.Errorf("running binwalk -e on %s: %w: %s", path, err, stderr.String())
	}
	data, err := os.ReadFile(log)
	if err != nil {
		return Analysis{}, fmt.Errorf("%w: on %s: %w", ErrBadJSON, path, err)
	}
	var res []struct{ Analysis Analysis }
	if err := json.Unmarshal(data, &res); err != nil || len(res) != 1 {
		return Analysis{}, fmt.Errorf("%w: on %s: no analysis", ErrBadJSON, path)
	}
	return res[0].Analysis, nil
}
