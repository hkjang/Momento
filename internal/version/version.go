package version

// Values are overridden by release builds through -ldflags.
var (
	Version   = "0.15.0-dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

type Info struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

func Current() Info {
	return Info{Name: "Momento", Version: Version, Commit: Commit, BuildTime: BuildTime}
}
