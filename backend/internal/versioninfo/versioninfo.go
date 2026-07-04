package versioninfo

import "strings"

var Version = "dev"

var Commit = "unknown"

var BuildDate = ""

const APIVersion = "0.1"

type Info struct {
	Version    string
	Commit     string
	BuildDate  string
	APIVersion string
}

func Current() Info {
	return Info{
		Version:    strings.TrimSpace(Version),
		Commit:     strings.TrimSpace(Commit),
		BuildDate:  strings.TrimSpace(BuildDate),
		APIVersion: APIVersion,
	}
}

func UserAgent(product string) string {
	trimmedProduct := strings.TrimSpace(product)
	if trimmedProduct == "" {
		trimmedProduct = "coyote"
	}
	version := strings.TrimSpace(Version)
	if version == "" {
		version = "dev"
	}
	return trimmedProduct + "/" + version
}
