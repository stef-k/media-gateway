package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// buildMetadata reports Go's embedded module/VCS provenance without host paths.
func buildMetadata() string {
	version, revision, modified := "(devel)", "unknown", "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" {
			version = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				modified = setting.Value
			}
		}
	}
	return fmt.Sprintf("media-gateway version=%s revision=%s modified=%s go=%s", version, revision, modified, runtime.Version())
}
