package release

import "regexp"

type Config struct {
	Releases []Release `yaml:"releases"`
}

type Release struct {
	ID        string         `yaml:"id"`
	Src       string         `yaml:"src"`
	Dst       string         `yaml:"dst"`
	Unpack    UnpackFormat   `yaml:"unpack,omitempty"`
	Regex     *regexp.Regexp `yaml:"regex,omitempty"`
	Integrity *Integrity     `yaml:"integrity,omitempty"`
	Assets    []Asset        `yaml:"assets,omitempty"`
}

type Asset struct {
	Src string `yaml:"src"`
	Dst string `yaml:"dst"`
}

type Integrity struct {
	PubKey    string       `yaml:"key"`
	Signature string       `yaml:"signature"`
	Bundle    string       `yaml:"bundle"`
	Algorithm SigAlgorithm `yaml:"algorithm"`
}

type UnpackFormat string

const (
	UnpackTarGz UnpackFormat = "tar.gz"
	UnpackTar   UnpackFormat = "tar"
	UnpackZip   UnpackFormat = "zip"
)

type SigAlgorithm string

const (
	AlgorithmPGP    SigAlgorithm = "pgp"
	AlgorithmCosign SigAlgorithm = "cosign"
	AlgorithmBundle SigAlgorithm = "bundle"
	AlgorithmSHA256 SigAlgorithm = "sha256"
)
