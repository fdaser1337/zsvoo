package types

const PackageMetadataFile = ".zsvo.yml"

// PkgInfo represents package metadata
type PkgInfo struct {
	Name         string   `yaml:"name"`
	Version      string   `yaml:"version"`
	Description  string   `yaml:"description,omitempty"`
	Dependencies []string `yaml:"deps,omitempty"`
	Files        []string `yaml:"files"`
	InstallDate  string   `yaml:"install_date"`
}
