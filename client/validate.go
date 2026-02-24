package client

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrInvalidCommand indicates a command failed security validation.
var ErrInvalidCommand = errors.New("invalid command")

// shellMetaChars are characters with special meaning in shells that must not
// appear in command names. Rejecting these prevents command injection when
// the command string is passed to exec.Command.
var shellMetaChars = []string{
	";", "|", "&", "$", "`",
	"(", ")", "{", "}", "[", "]",
	"<", ">", "!", "~", "*", "?",
	"\\", "'", "\"", "\n", "\r",
}

// argMetaChars are shell metacharacters that must not appear in arguments.
const argMetaChars = ";|&$`"

// ValidateCommand validates a command string for safe use with exec.Command.
// It returns the resolved absolute path to the executable.
//
// Security: This function prevents command injection by:
//  1. Rejecting empty commands
//  2. Rejecting commands containing shell metacharacters
//  3. Rejecting absolute paths with directory traversal
//  4. Resolving to absolute paths via exec.LookPath
func ValidateCommand(cmd string) (string, error) {
	if cmd == "" {
		return "", fmt.Errorf("%w: empty command", ErrInvalidCommand)
	}

	for _, char := range shellMetaChars {
		if strings.Contains(cmd, char) {
			return "", fmt.Errorf("%w: contains shell metacharacter %q", ErrInvalidCommand, char)
		}
	}

	if filepath.IsAbs(cmd) {
		cleanPath := filepath.Clean(cmd)
		if cleanPath != cmd {
			return "", fmt.Errorf("%w: path contains traversal elements", ErrInvalidCommand)
		}

		resolved, err := exec.LookPath(cmd)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrInvalidCommand, err)
		}
		return resolved, nil
	}

	resolved, err := exec.LookPath(cmd)
	if err != nil {
		return "", fmt.Errorf("%w: command not found: %w", ErrInvalidCommand, err)
	}

	return resolved, nil
}

// ValidateArgs checks that none of the arguments contain dangerous shell
// metacharacters that could enable injection.
func ValidateArgs(args []string) error {
	for i, arg := range args {
		if strings.ContainsAny(arg, argMetaChars) {
			return fmt.Errorf("%w: argument %d contains shell metacharacter", ErrInvalidCommand, i)
		}
	}
	return nil
}
