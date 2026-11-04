//go:build factorybox_candidate_amd64 || factorybox_candidate_arm64

package platform

// The native candidate runner links the kernel it executes on. It labels that
// exact kernel verified. Admission uses the same capability gate as ordinary
// builds.
var candidateKernel string

func linkedKernel() string       { return candidateKernel }
func linkedArchitecture() string { return candidateArchitecture }
