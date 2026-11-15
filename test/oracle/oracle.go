// Package oracle starts the reference libmagic of D33 and asks it questions.
//
// The reference is a program of its own, which test/oracle/build.sh builds
// into test/.oracle/bin/oracle. A test that needs it calls Open, which skips
// the test when the program is not built. See D30 in docs/decisions.
package oracle

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// Flags of libmagic that the tests use. They have the values of magic.h of
// file 5.48.
const (
	None         = 0x0000000
	Compress     = 0x0000004
	MimeType     = 0x0000010
	Continue     = 0x0000020
	Raw          = 0x0000100
	Error        = 0x0000200
	MimeEncoding = 0x0000400
	Mime         = MimeType | MimeEncoding
	Apple        = 0x0000800
	Extension    = 0x1000000
)

// Question is one question for the oracle. Op is "buffer", which reads Data,
// or "file", which reads the file at Path. Magic names the rule files to load,
// joined by ':', or is empty for the database of file 5.48.
type Question struct {
	Op    string `json:"op"`
	Flags int    `json:"flags"`
	Data  []byte `json:"data,omitempty"`
	Path  string `json:"path,omitempty"`
	Magic string `json:"magic,omitempty"`
}

// Answer is what libmagic said. Err is the text of magic_error when libmagic
// returned no text, and is empty otherwise.
type Answer struct {
	Text string
	Err  string
}

// Oracle is a running oracle. It is safe for concurrent use, and it answers
// one question at a time.
type Oracle struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Scanner
	next int
}

// Dir returns the folder that build.sh writes to.
func Dir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", ".oracle")
}

// Start starts the oracle program at path.
func Start(path string) (*Oracle, error) {
	cmd := exec.Command(path)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("starting the oracle: %w", err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("starting the oracle: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting the oracle %s: %w", path, err)
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<16), 64<<20)
	return &Oracle{cmd: cmd, in: in, out: sc}, nil
}

// Open starts the oracle for a test, and stops it when the test ends. It
// skips the test when the oracle is not built.
func Open(t testing.TB) *Oracle {
	t.Helper()
	path := filepath.Join(Dir(), "bin", "oracle")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("the oracle is not built. Run test/oracle/build.sh")
	}
	o, err := Start(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := o.Close(); err != nil {
			t.Errorf("stopping the oracle: %v", err)
		}
	})
	return o
}

// Ask asks one question. An error means that the oracle failed, not that
// libmagic did. A failure of libmagic is in Answer.Err.
func (o *Oracle) Ask(q Question) (Answer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.next++
	id := o.next
	req := struct {
		ID int `json:"id"`
		Question
	}{id, q}
	b, err := json.Marshal(req)
	if err != nil {
		return Answer{}, fmt.Errorf("encoding question %d: %w", id, err)
	}
	if _, err := o.in.Write(append(b, '\n')); err != nil {
		return Answer{}, fmt.Errorf("sending question %d: %w", id, err)
	}
	if !o.out.Scan() {
		if err := o.out.Err(); err != nil {
			return Answer{}, fmt.Errorf("reading answer %d: %w", id, err)
		}
		return Answer{}, fmt.Errorf("reading answer %d: %w", id, io.ErrUnexpectedEOF)
	}
	var res struct {
		ID    int    `json:"id"`
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(o.out.Bytes(), &res); err != nil {
		return Answer{}, fmt.Errorf("decoding answer %d: %w", id, err)
	}
	if res.ID != id {
		return Answer{}, fmt.Errorf("reading answer %d: got the answer to %d", id, res.ID)
	}
	return Answer{Text: res.Text, Err: res.Error}, nil
}

// Buffer asks what libmagic says of data with flags.
func (o *Oracle) Buffer(data []byte, flags int) (Answer, error) {
	return o.Ask(Question{Op: "buffer", Flags: flags, Data: data})
}

// File asks what libmagic says of the file at path with flags.
func (o *Oracle) File(path string, flags int) (Answer, error) {
	return o.Ask(Question{Op: "file", Flags: flags, Path: path})
}

// Close stops the oracle.
func (o *Oracle) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.in.Close(); err != nil {
		return fmt.Errorf("closing the input of the oracle: %w", err)
	}
	if err := o.cmd.Wait(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("the oracle stopped with %w", err)
		}
		return fmt.Errorf("waiting for the oracle: %w", err)
	}
	return nil
}
