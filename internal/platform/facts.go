// Package platform defines host facts and pure matching semantics shared by
// detection, configuration, and execution layers.
package platform

// Facts describes observable properties of the target host. Detection fills
// this structure; classification such as ResolveFamily remains a separate,
// pure step so consumers can distinguish observed facts from derived policy.
//
// Field values are an internal compatibility contract because they are also
// exposed through schema placeholders. In particular, TargetArch retains the
// uname-style spellings historically emitted by the host detector (for
// example x86_64 and aarch64) on the full native-detection path.
type Facts struct {
	TargetArch      string `json:"target_arch"`
	DistroID        string `json:"distro_id"`
	DistroName      string `json:"distro_name"`
	DistroVersion   string `json:"distro_version"`
	DistroIDLike    string `json:"distro_id_like"`
	TargetFamily    string `json:"target_family"`
	DetectionMethod string `json:"detection_method"`
	Confidence      string `json:"confidence"`
	IsWSL           bool   `json:"is_wsl"`
	IsContainer     bool   `json:"is_container"`
	IsAndroid       bool   `json:"is_android"`
	Kernel          string `json:"kernel"`
	Libc            string `json:"libc"`
	InitSystem      string `json:"init_system"`
	OS              string `json:"os"`
}
