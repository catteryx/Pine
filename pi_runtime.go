package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
)

// How Pine chooses the pi runtime it runs.
const (
	piSourceAuto    = ""
	piSourceBundled = "bundled"
	piSourceSystem  = "system"
)

// piSourceLabels are the settings choices, in order.
var piSourceLabels = []string{"Automatic", "Bundled", "System"}

func piSourceIndex(source string) int {
	switch source {
	case piSourceBundled:
		return 1
	case piSourceSystem:
		return 2
	}
	return 0
}

func piSourceAt(index int) string {
	switch index {
	case 1:
		return piSourceBundled
	case 2:
		return piSourceSystem
	}
	return piSourceAuto
}

// bundledPiDir is the directory of the pi runtime a release build ships, or ""
// when this build has none. In an app bundle it is Contents/Resources/pi.
func bundledPiDir() string {
	dir, err := mygo.App.Path(mygo.PathResources)
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "pi")
}

// bundledPiPath returns the bundled pi executable, or "" when absent. The
// executable sits next to the assets pi reads at runtime (theme, export-html,
// docs and the image wasm), which is the layout of pi's release archive.
func bundledPiPath() string {
	name := "pi"
	if runtime.GOOS == "windows" {
		name = "pi.exe"
	}
	path := filepath.Join(bundledPiDir(), name)
	if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return path
	}
	return ""
}

// bundledPiVersion names the bundled pi release, for the settings window.
func bundledPiVersion() string {
	data, err := os.ReadFile(filepath.Join(bundledPiDir(), "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	return pkg.Version
}

// applyPiSettings picks the executable Pine runs: PI_BIN wins, then an explicit
// path, then the chosen source. The bundled runtime is preferred when present,
// so a release build needs no setup; a build without it runs the user's pi.
func (a *app) applyPiSettings() {
	switch {
	case os.Getenv("PI_BIN") != "":
		a.piPath = os.Getenv("PI_BIN")
	case a.settings.PiBin != "":
		a.piPath = a.settings.PiBin
	case a.settings.PiSource == piSourceSystem:
		a.piPath = "pi"
	default:
		if path := bundledPiPath(); path != "" {
			a.piPath = path
		} else {
			a.piPath = "pi"
		}
	}
}

// piSourceHint describes, in the settings window, which pi the choice runs.
func piSourceHint(a *app) string {
	version := bundledPiVersion()
	if version != "" {
		if a.settings.PiSource == piSourceSystem {
			return "Which pi to run. This build also bundles pi " + version + "."
		}
		return "Which pi to run. This build bundles pi " + version + "."
	}
	if a.settings.PiSource == piSourceBundled {
		return "Which pi to run. This build has no bundled pi, so yours is used."
	}
	return "Which pi to run. This build has no bundled pi, so Pine uses yours."
}
