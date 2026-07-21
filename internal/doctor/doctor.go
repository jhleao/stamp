package doctor

import (
	"os"
	"os/exec"
	"runtime"

	stampdrive "github.com/weve-ai/stamp/internal/drive"
	"github.com/weve-ai/stamp/internal/render"
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
	config := stampdrive.ConfigPath()
	_, err = os.Stat(config)
	checks = append(checks, Check{Name: "Google OAuth config", OK: err == nil, Detail: config})
	return checks
}

func executable(name string, path string, err error) Check {
	if err != nil {
		return Check{Name: name, Detail: err.Error()}
	}
	return Check{Name: name, OK: true, Detail: path}
}
