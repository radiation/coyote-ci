package domain

// ImageSourceKind identifies whether execution used an external registry image
// or a Coyote-managed image version.
type ImageSourceKind string

const (
	ImageSourceKindExternal ImageSourceKind = "external"
	ImageSourceKindManaged  ImageSourceKind = "managed"
)
