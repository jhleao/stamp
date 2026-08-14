package doctor

import (
	"os"
	"os/exec"
	"runtime"

	stampdrive "github.com/jhleao/stamp/internal/drive"
	"github.com/jhleao/stamp/internal/render"
	"github.com/jhleao/stamp/internal/theme"
)

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func Run() []Check {
	checks := []Check{{Name: "macOS", OK: runtime.GOOS == "darwin", Detail: runtime.GOOS}}
	path, err := render.ChromePath()
	checks = append(checks, executable("Chrome", path, err))
	path, err = exec.LookPath("soffice")
	checks = append(checks, executable("LibreOffice", path, err))
	path, err = exec.LookPath("pandoc")
	checks = append(checks, executable("Pandoc", path, err))
	root, _ := os.Getwd()
	path, _ = theme.Compiler(root)
	if path == "" {
		err = exec.ErrNotFound
	} else {
		err = nil
	}
	checks = append(checks, executable("Tailwind", path, err))
	credentials, credentialErr := stampdrive.Credentials()
	detail := string(credentials.Source)
	if credentials.Path != "" {
		detail += " (" + credentials.Path + ")"
	}
	checks = append(checks, Check{Name: "Google OAuth", OK: credentialErr == nil, Detail: detail})
	return checks
}

func executable(name string, path string, err error) Check {
	if err != nil {
		return Check{Name: name, Detail: err.Error()}
	}
	return Check{Name: name, OK: true, Detail: path}
}
