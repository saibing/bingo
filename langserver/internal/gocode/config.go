package gocode

//-------------------------------------------------------------------------
// config
//
// Structure represents persistent config storage of the gocode daemon. Usually
// the config is located somewhere in ~/.config/gocode directory.
//-------------------------------------------------------------------------

type config struct {
	ProposeBuiltins    bool   `json:"propose-builtins"`
	LibPath            string `json:"lib-path"`
	CustomPkgPrefix    string `json:"custom-pkg-prefix"`
	CustomVendorDir    string `json:"custom-vendor-dir"`
	Autobuild          bool   `json:"autobuild"`
	ForceDebugOutput   string `json:"force-debug-output"`
	PackageLookupMode  string `json:"package-lookup-mode"`
	CloseTimeout       int    `json:"close-timeout"`
	UnimportedPackages bool   `json:"unimported-packages"`
}

var g_config = config{
	ProposeBuiltins:    false,
	LibPath:            "",
	CustomPkgPrefix:    "",
	Autobuild:          false,
	ForceDebugOutput:   "",
	PackageLookupMode:  "go",
	CloseTimeout:       1800,
	UnimportedPackages: false,
}


