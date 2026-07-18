package sparkle

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseScriptKeepsPrivateKeyOnStdin(t *testing.T) {
	data, err := os.ReadFile("release.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{"--ed-key-file -", "IFS= read -r private_key", "unset private_key", "dist/.update.staging", "dist/.update.lock", "atomicreplace", "verify_release.sh"} {
		if !strings.Contains(script, required) {
			t.Errorf("release.sh missing %q", required)
		}
	}
	for _, forbidden := range []string{"generate_keys", "PRIVATE_KEY=", "-s \"$private_key\""} {
		if strings.Contains(script, forbidden) {
			t.Errorf("release.sh contains forbidden private-key path %q", forbidden)
		}
	}
}

func TestReleaseTaskArgumentsCannotExecuteShellSubstitutions(t *testing.T) {
	task, err := exec.LookPath("task")
	if err != nil {
		t.Skip("task CLI is unavailable")
	}
	canary := filepath.Join(t.TempDir(), "injected")
	payload := "$(touch${IFS}" + canary + ")"
	publicKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	command := exec.Command(task, "--silent", "release:verify",
		"APP_VERSION="+payload,
		"BUILD_NUMBER=1",
		"FEED_URL=https://updates.example.test/appcast.xml",
		"DOWNLOAD_URL_PREFIX=https://downloads.example.test",
		"PUBLIC_KEY="+publicKey,
	)
	command.Dir = filepath.Clean(filepath.Join("..", ".."))
	if err := command.Run(); err == nil {
		t.Fatal("malicious version unexpectedly verified")
	}
	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Fatal("Task template executed command substitution")
	}
}
