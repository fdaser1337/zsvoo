package types

// PkgInfo represents package metadata
type PkgInfo struct {
	Name        string   `toml:"name"`
	Version     string   `toml:"version"`
	Description string   `toml:"description,omitempty"`
	Dependencies []string `toml:"dependencies,omitempty"`
	Files       []string `toml:"files"`
	InstallDate string   `toml:"install_date"`
}
