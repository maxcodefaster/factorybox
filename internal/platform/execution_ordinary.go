//go:build !factorybox_candidate_amd64 && !factorybox_candidate_arm64

package platform

// Ordinary builds link no test kernel.
func linkedKernel() string       { return "" }
func linkedArchitecture() string { return "" }
