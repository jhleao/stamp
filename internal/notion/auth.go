package notion

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const keychainService = "com.stamp.notion"
const keychainAccount = "personal-access-token"

// Login validates before replacing the saved credential. Never pass credentials
// as process arguments or include security's output in errors.
func Login(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("enter a Notion personal access token")
	}
	if _, err := newWithToken(ctx, token); err != nil {
		return fmt.Errorf("Notion token validation failed: %w", err)
	}
	if runtime.GOOS != "darwin" {
		return errors.New("persistent Notion login requires macOS Keychain; use STAMP_NOTION_TOKEN on this platform")
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "-i")
	// Hex encoding avoids security's interactive command parser interpreting any
	// credential characters. The command and secret travel only through stdin.
	cmd.Stdin = strings.NewReader("add-generic-password -U -s " + keychainService + " -a " + keychainAccount + " -X " + hex.EncodeToString([]byte(token)) + "\n")
	if err := cmd.Run(); err != nil {
		return errors.New("could not save Notion token in Keychain; unlock your login keychain and retry")
	}
	saved, err := savedToken(ctx)
	if err != nil || saved != token {
		return errors.New("could not verify the Notion token in Keychain; unlock your login keychain and retry")
	}
	return nil
}

func savedToken(ctx context.Context) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.New("run stamp login notion on macOS, or set STAMP_NOTION_TOKEN")
	}
	output, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", keychainService, "-a", keychainAccount, "-w").Output()
	if err != nil {
		return "", errors.New("Notion token is unavailable; unlock your login keychain or run stamp login notion")
	}
	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", errors.New("no saved Notion token; run stamp login notion")
	}
	return token, nil
}

func tokenForSession(ctx context.Context) (string, error) {
	if token := strings.TrimSpace(os.Getenv("STAMP_NOTION_TOKEN")); token != "" {
		return token, nil
	}
	return savedToken(ctx)
}

// Logout removes the local credential; token revocation belongs to Notion.
func Logout(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return errors.New("persistent Notion login requires macOS Keychain")
	}
	err := exec.CommandContext(ctx, "/usr/bin/security", "delete-generic-password", "-s", keychainService, "-a", keychainAccount).Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 44 {
		return nil
	} // Already absent.
	if err != nil {
		return errors.New("could not remove the Notion token from Keychain; unlock your login keychain and retry")
	}
	return nil
}
