//go:build !linux

package process

import "os/exec"

func configureProcessGroup(_ *exec.Cmd) {}
