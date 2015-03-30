package token

import (
	"bytes"
	"fmt"
	"os/exec"
)

// Helper is the struct that has all the logic for storing and retrieving
// tokens from the token helper. The API for the helpers is simple: the
// Path is executed within a shell. The last argument appended will be the
// operation, which is:
//
//   * "get" - Read the value of the token and write it to stdout.
//   * "store" - Store the value of the token which is on stdin. Output
//       nothing.
//   * "erase" - Erase the contents stored. Output nothing.
//
// Any errors can be written on stdout. If the helper exits with a non-zero
// exit code then the stderr will be made part of the error value.
type Helper struct {
	Path string
}

// Erase deletes the contents from the helper.
func (h *Helper) Erase() error {
	cmd := h.cmd("erase")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf(
			"Error: %s\n\n%s", err, string(output))
	}

	return nil
}

// Get gets the token value from the helper.
func (h *Helper) Get() (string, error) {
	var buf, stderr bytes.Buffer
	cmd := h.cmd("get")
	cmd.Stdout = &buf
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf(
			"Error: %s\n\n%s", err, stderr.String())
	}

	return buf.String(), nil
}

// Store stores the token value into the helper.
func (h *Helper) Store(v string) error {
	buf := bytes.NewBufferString(v)
	cmd := h.cmd("store")
	cmd.Stdin = buf
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf(
			"Error: %s\n\n%s", err, string(output))
	}

	return nil
}

func (h *Helper) cmd(op string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", h.Path+" "+op)
	return cmd
}