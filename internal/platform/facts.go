// Package platform defines host facts and pure matching semantics shared by
// detection, configuration, and execution layers.
package platform

// Facts mirrors detect_os.sh's JSON output without coupling consumers to the
// engine that gathers it.
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
