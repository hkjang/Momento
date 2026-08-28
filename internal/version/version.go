package version

// Version is the single declaration of what this source tree is. Every other
// place that names a version — the console's package.json, the SDK's, the image
// tag a release builds — is checked against it by TestDeclaredVersionsAgree, so
// they cannot drift apart without a test saying so.
//
// A build that does not stamp anything still reports this, because a service that
// cannot say which version it is running is worse than one that says it without a
// commit. Commit and BuildTime are what separate a release build from a source
// build; the console prints "개발 빌드" when the commit is unknown.
//
// Commit and BuildTime are overridden by release builds through -ldflags. Version
// is overridden too, so a release image reports the tag it was cut from even if
// this line were left behind.
var (
	Version   = "0.34.8"
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
